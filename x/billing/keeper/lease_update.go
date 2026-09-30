package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"strconv"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/pkg/sanitize"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// The lease-update handshake gives every manifest version — not just the one
// supplied at lease creation — its own on-chain commitment, mirroring the
// PENDING → ACTIVE handshake the lease itself goes through:
//
//	tenant   RequestLeaseUpdate      → lease.PendingMetaHash = h
//	tenant   uploads the payload off-chain
//	provider validates, persists and applies it
//	provider AcknowledgeLeaseUpdate  → lease.MetaHash = h, revision++
//
// The committed MetaHash only advances at the last step, so the payload the
// provider is already serving stays valid against it for the whole window. An
// update that never completes therefore cannot leave the lease unable to
// reprovision — which is the property a single "just overwrite the hash"
// message could not provide.
//
// A pending request is inert: it never affects MetaHash, so the chain does not
// expire it and no EndBlocker work is added. Providers apply their own
// staleness policy over PendingMetaHashAt.

// clearPendingLeaseUpdate drops an in-flight update request. The index entry is
// reconciled by SetLease, so callers only need to clear the fields.
func clearPendingLeaseUpdate(lease *types.Lease) {
	lease.PendingMetaHash = nil
	lease.PendingMetaHashAt = nil
}

// requireActiveLeaseForUpdate loads a lease and rejects it unless it is ACTIVE.
// Update requests only exist on ACTIVE leases: a PENDING lease still has its
// create-time handshake, where changing the hash under a provider that is about
// to acknowledge would race the acknowledgement. A tenant who committed a wrong
// hash before acknowledgement cancels the lease and creates a new one.
func (k *Keeper) requireActiveLeaseForUpdate(ctx context.Context, leaseUUID string) (types.Lease, error) {
	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil {
		return types.Lease{}, err
	}
	if lease.State != types.LEASE_STATE_ACTIVE {
		return types.Lease{}, types.ErrLeaseNotActive.Wrapf("lease %s is in state %s", leaseUUID, lease.State)
	}
	return lease, nil
}

// requirePendingUpdate checks that the lease has a pending update matching
// metaHash. The hash is required on the provider-side verbs so a request the
// provider has not evaluated — one the tenant submitted after the provider read
// the lease — cannot be acknowledged or rejected by accident.
func requirePendingUpdate(lease types.Lease, metaHash []byte) error {
	if len(lease.PendingMetaHash) == 0 {
		return types.ErrNoPendingLeaseUpdate.Wrapf("lease %s has no pending update", lease.Uuid)
	}
	if !bytes.Equal(lease.PendingMetaHash, metaHash) {
		return types.ErrLeaseUpdateMismatch.Wrapf(
			"lease %s has pending update %s, not %s",
			lease.Uuid,
			hex.EncodeToString(lease.PendingMetaHash),
			hex.EncodeToString(metaHash),
		)
	}
	return nil
}

// RequestLeaseUpdate records the tenant's requested next manifest hash on an
// ACTIVE lease, superseding any request the provider has not acted on yet.
//
// Returns the role the call was authorised under ("tenant" / "authority" /
// "allowed") so the caller can record it on the emitted event, and the
// effective PendingMetaHashAt — which on an idempotent re-request is the
// ORIGINAL request's timestamp, not the current block time.
//
// Named Request… rather than UpdateLease so it is never mistaken for SetLease
// at the keeper layer; the message is still MsgUpdateLease.
func (k *Keeper) RequestLeaseUpdate(ctx context.Context, sender, leaseUUID string, metaHash []byte) (string, time.Time, error) {
	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil {
		return "", time.Time{}, err
	}

	role, err := k.IsAuthorizedForTenant(ctx, sender, lease.Tenant)
	if err != nil {
		return "", time.Time{}, err
	}
	if role == "" {
		return "", time.Time{}, types.ErrUnauthorized.Wrapf("sender %s is not authorised to update lease %s", sender, leaseUUID)
	}

	if lease.State != types.LEASE_STATE_ACTIVE {
		return "", time.Time{}, types.ErrLeaseNotActive.Wrapf("lease %s is in state %s", leaseUUID, lease.State)
	}

	if len(metaHash) == 0 {
		return "", time.Time{}, types.ErrInvalidMetaHash.Wrap("meta_hash cannot be empty")
	}
	if len(metaHash) > types.MaxMetaHashLength {
		return "", time.Time{}, types.ErrInvalidMetaHash.Wrapf("meta_hash exceeds maximum length of %d bytes", types.MaxMetaHashLength)
	}

	// Idempotent re-request: the same hash is already pending. No state change
	// and no event, so the audit log does not record a non-change. Refreshing
	// PendingMetaHashAt here would also let a tenant reset the provider's
	// staleness clock for free — hence returning the stored timestamp.
	//
	// A set hash with a nil timestamp is corrupt state no handler can produce
	// (SetLease normalises, and every write here sets both). Fall through to
	// the write path so the record repairs itself rather than dereferencing nil.
	if bytes.Equal(lease.PendingMetaHash, metaHash) && lease.PendingMetaHashAt != nil {
		return role, *lease.PendingMetaHashAt, nil
	}

	superseded := lease.PendingMetaHash

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	requestedAt := sdkCtx.BlockTime()

	lease.PendingMetaHash = metaHash
	lease.PendingMetaHashAt = &requestedAt
	if err := k.SetLease(ctx, lease); err != nil {
		return "", time.Time{}, err
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
		sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
		sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
		sdk.NewAttribute(types.AttributeKeyPendingMetaHash, hex.EncodeToString(metaHash)),
		sdk.NewAttribute(types.AttributeKeyMetaHashRevision, strconv.FormatUint(lease.MetaHashRevision, 10)),
		sdk.NewAttribute(types.AttributeKeyRequestedBy, role),
	}
	// Only present when this request replaced one the provider never acted on,
	// so an indexer can tell a supersession from a fresh request.
	if len(superseded) > 0 {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeySupersededMetaHash, hex.EncodeToString(superseded)))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeLeaseUpdateRequested, attrs...))

	return role, requestedAt, nil
}

// AcknowledgeLeaseUpdate promotes the lease's pending update to the committed
// meta_hash and increments the revision. The provider sends this only after it
// has validated, persisted and applied the matching payload, so the committed
// hash always names a manifest the provider has actually taken on.
//
// Returns the lease's revision after promotion.
func (k *Keeper) AcknowledgeLeaseUpdate(ctx context.Context, sender, leaseUUID string, metaHash []byte) (uint64, error) {
	lease, err := k.requireActiveLeaseForUpdate(ctx, leaseUUID)
	if err != nil {
		return 0, err
	}

	if _, err := k.ValidateProviderAuthorization(ctx, sender, lease.ProviderUuid, "acknowledge updates for"); err != nil {
		return 0, err
	}

	if err := requirePendingUpdate(lease, metaHash); err != nil {
		return 0, err
	}

	previous := lease.MetaHash

	lease.MetaHash = lease.PendingMetaHash
	lease.MetaHashRevision++
	clearPendingLeaseUpdate(&lease)
	if err := k.SetLease(ctx, lease); err != nil {
		return 0, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseUpdateAcknowledged,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyMetaHash, hex.EncodeToString(lease.MetaHash)),
			sdk.NewAttribute(types.AttributeKeyPreviousMetaHash, hex.EncodeToString(previous)),
			sdk.NewAttribute(types.AttributeKeyMetaHashRevision, strconv.FormatUint(lease.MetaHashRevision, 10)),
			sdk.NewAttribute(types.AttributeKeyAcknowledgedBy, sender),
		),
	)

	return lease.MetaHashRevision, nil
}

// RejectLeaseUpdate discards a pending update the provider will not apply,
// leaving the committed meta_hash untouched. Rejecting explicitly — rather than
// simply never acknowledging — is what lets the tenant tell refusal from delay.
func (k *Keeper) RejectLeaseUpdate(ctx context.Context, sender, leaseUUID string, metaHash []byte, reason string) error {
	lease, err := k.requireActiveLeaseForUpdate(ctx, leaseUUID)
	if err != nil {
		return err
	}

	if _, err := k.ValidateProviderAuthorization(ctx, sender, lease.ProviderUuid, "reject updates for"); err != nil {
		return err
	}

	if err := requirePendingUpdate(lease, metaHash); err != nil {
		return err
	}

	clearPendingLeaseUpdate(&lease)
	if err := k.SetLease(ctx, lease); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseUpdateRejected,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyPendingMetaHash, hex.EncodeToString(metaHash)),
			sdk.NewAttribute(types.AttributeKeyReason, sanitize.EventAttribute(reason)),
			sdk.NewAttribute(types.AttributeKeyRejectedBy, sender),
		),
	)

	return nil
}

// CancelLeaseUpdate withdraws a pending update the tenant no longer wants.
// Returns the role the call was authorised under.
//
// Unlike the provider-side verbs this takes no meta_hash: the tenant authored
// the request and always means the one currently pending.
func (k *Keeper) CancelLeaseUpdate(ctx context.Context, sender, leaseUUID string) (string, error) {
	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil {
		return "", err
	}

	role, err := k.IsAuthorizedForTenant(ctx, sender, lease.Tenant)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", types.ErrUnauthorized.Wrapf("sender %s is not authorised to cancel updates on lease %s", sender, leaseUUID)
	}

	if lease.State != types.LEASE_STATE_ACTIVE {
		return "", types.ErrLeaseNotActive.Wrapf("lease %s is in state %s", leaseUUID, lease.State)
	}

	if len(lease.PendingMetaHash) == 0 {
		return "", types.ErrNoPendingLeaseUpdate.Wrapf("lease %s has no pending update", leaseUUID)
	}

	cancelled := lease.PendingMetaHash
	clearPendingLeaseUpdate(&lease)
	if err := k.SetLease(ctx, lease); err != nil {
		return "", err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseUpdateCancelled,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyPendingMetaHash, hex.EncodeToString(cancelled)),
			sdk.NewAttribute(types.AttributeKeyCancelledBy, role),
		),
	)

	return role, nil
}
