package keeper

import (
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	collcodec "cosmossdk.io/collections/codec"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/internal/storagecodec"
	billingstorage "github.com/manifest-network/manifest-ledger/x/billing/internal/types"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Billing API protobufs use Bech32 strings at the wire boundary. These value
// codecs retain those public Go types while persisting addresses as raw bytes.
// A tag-zero prefix is an unambiguous format discriminator: field number zero
// is invalid protobuf, so no legacy CollValue payload can begin with it.
const (
	paramsStoragePrefix        = "\x00billing/params/v1"
	leaseStoragePrefix         = "\x00billing/lease/v1"
	creditAccountStoragePrefix = "\x00billing/credit-account/v1"
)

func newParamsValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Params] {
	wire := sdkcodec.CollValue[types.Params](cdc)
	return storagecodec.Value[types.Params]{
		Wire:   wire,
		Module: types.ModuleName,
		Prefix: paramsStoragePrefix,
		EncodeStorage: func(params types.Params) ([]byte, error) {
			allowedAddresses, err := decodeAddressStrings(params.AllowedList)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed-list address: %w", err)
			}

			return marshalStorage(&billingstorage.Params{
				MaxLeasesPerTenant:        params.MaxLeasesPerTenant,
				AllowedAddresses:          allowedAddresses,
				MaxItemsPerLease:          params.MaxItemsPerLease,
				MinLeaseDuration:          params.MinLeaseDuration,
				MaxPendingLeasesPerTenant: params.MaxPendingLeasesPerTenant,
				PendingTimeout:            params.PendingTimeout,
				ReservedDomainSuffixes:    append([]string(nil), params.ReservedDomainSuffixes...),
			})
		},
		DecodeStorage: func(encoded []byte) (types.Params, error) {
			var stored billingstorage.Params
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Params{}, err
			}

			allowedList, err := encodeAddressBytes(stored.AllowedAddresses)
			if err != nil {
				return types.Params{}, fmt.Errorf("invalid stored allowed-list address: %w", err)
			}
			return types.Params{
				MaxLeasesPerTenant:        stored.MaxLeasesPerTenant,
				AllowedList:               allowedList,
				MaxItemsPerLease:          stored.MaxItemsPerLease,
				MinLeaseDuration:          stored.MinLeaseDuration,
				MaxPendingLeasesPerTenant: stored.MaxPendingLeasesPerTenant,
				PendingTimeout:            stored.PendingTimeout,
				ReservedDomainSuffixes:    append([]string(nil), stored.ReservedDomainSuffixes...),
			}, nil
		},
		NormalizeLegacy: normalizeParamsAddresses,
	}
}

func newLeaseValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Lease] {
	wire := sdkcodec.CollValue[types.Lease](cdc)
	return storagecodec.Value[types.Lease]{
		Wire:   wire,
		Module: types.ModuleName,
		Prefix: leaseStoragePrefix,
		EncodeStorage: func(lease types.Lease) ([]byte, error) {
			tenantAddr, err := sdk.AccAddressFromBech32(lease.Tenant)
			if err != nil {
				return nil, fmt.Errorf("invalid lease tenant address: %w", err)
			}

			return marshalStorage(&billingstorage.Lease{
				Uuid:                       lease.Uuid,
				TenantAddress:              append([]byte(nil), tenantAddr.Bytes()...),
				ProviderUuid:               lease.ProviderUuid,
				Items:                      append([]types.LeaseItem(nil), lease.Items...),
				State:                      lease.State,
				CreatedAt:                  lease.CreatedAt,
				ClosedAt:                   lease.ClosedAt,
				LastSettledAt:              lease.LastSettledAt,
				AcknowledgedAt:             lease.AcknowledgedAt,
				RejectedAt:                 lease.RejectedAt,
				RejectionReason:            lease.RejectionReason,
				ExpiredAt:                  lease.ExpiredAt,
				ClosureReason:              lease.ClosureReason,
				MetaHash:                   append([]byte(nil), lease.MetaHash...),
				MinLeaseDurationAtCreation: lease.MinLeaseDurationAtCreation,
				Reservation:                cloneLeaseReservation(lease.Reservation),
			})
		},
		DecodeStorage: func(encoded []byte) (types.Lease, error) {
			var stored billingstorage.Lease
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Lease{}, err
			}

			tenant, err := accountAddressString(stored.TenantAddress)
			if err != nil {
				return types.Lease{}, fmt.Errorf("invalid stored lease tenant address: %w", err)
			}
			return types.Lease{
				Uuid:                       stored.Uuid,
				Tenant:                     tenant,
				ProviderUuid:               stored.ProviderUuid,
				Items:                      append([]types.LeaseItem(nil), stored.Items...),
				State:                      stored.State,
				CreatedAt:                  stored.CreatedAt,
				ClosedAt:                   stored.ClosedAt,
				LastSettledAt:              stored.LastSettledAt,
				AcknowledgedAt:             stored.AcknowledgedAt,
				RejectedAt:                 stored.RejectedAt,
				RejectionReason:            stored.RejectionReason,
				ExpiredAt:                  stored.ExpiredAt,
				ClosureReason:              stored.ClosureReason,
				MetaHash:                   append([]byte(nil), stored.MetaHash...),
				MinLeaseDurationAtCreation: stored.MinLeaseDurationAtCreation,
				Reservation:                cloneLeaseReservation(stored.Reservation),
			}, nil
		},
		NormalizeLegacy: normalizeLeaseAddresses,
	}
}

func newCreditAccountValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.CreditAccount] {
	wire := sdkcodec.CollValue[types.CreditAccount](cdc)
	return storagecodec.Value[types.CreditAccount]{
		Wire:   wire,
		Module: types.ModuleName,
		Prefix: creditAccountStoragePrefix,
		EncodeStorage: func(account types.CreditAccount) ([]byte, error) {
			tenantAddr, err := sdk.AccAddressFromBech32(account.Tenant)
			if err != nil {
				return nil, fmt.Errorf("invalid credit-account tenant address: %w", err)
			}
			creditAddr, err := sdk.AccAddressFromBech32(account.CreditAddress)
			if err != nil {
				return nil, fmt.Errorf("invalid credit address: %w", err)
			}

			return marshalStorage(&billingstorage.CreditAccount{
				TenantAddress:     append([]byte(nil), tenantAddr.Bytes()...),
				CreditAddress:     append([]byte(nil), creditAddr.Bytes()...),
				ActiveLeaseCount:  account.ActiveLeaseCount,
				PendingLeaseCount: account.PendingLeaseCount,
				ReservedAmounts:   append(sdk.Coins(nil), account.ReservedAmounts...),
				UnattributedReservedAmounts: append(
					sdk.Coins(nil),
					account.UnattributedReservedAmounts...,
				),
				UnattributedLeaseCount: account.UnattributedLeaseCount,
			})
		},
		DecodeStorage: func(encoded []byte) (types.CreditAccount, error) {
			var stored billingstorage.CreditAccount
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.CreditAccount{}, err
			}

			tenant, err := accountAddressString(stored.TenantAddress)
			if err != nil {
				return types.CreditAccount{}, fmt.Errorf("invalid stored credit-account tenant address: %w", err)
			}
			creditAddress, err := accountAddressString(stored.CreditAddress)
			if err != nil {
				return types.CreditAccount{}, fmt.Errorf("invalid stored credit address: %w", err)
			}

			return types.CreditAccount{
				Tenant:            tenant,
				CreditAddress:     creditAddress,
				ActiveLeaseCount:  stored.ActiveLeaseCount,
				PendingLeaseCount: stored.PendingLeaseCount,
				ReservedAmounts:   append(sdk.Coins(nil), stored.ReservedAmounts...),
				UnattributedReservedAmounts: append(
					sdk.Coins(nil),
					stored.UnattributedReservedAmounts...,
				),
				UnattributedLeaseCount: stored.UnattributedLeaseCount,
			}, nil
		},
		NormalizeLegacy: normalizeCreditAccountAddresses,
	}
}

func cloneLeaseReservation(reservation *types.LeaseReservation) *types.LeaseReservation {
	if reservation == nil {
		return nil
	}
	return &types.LeaseReservation{
		RemainingAmounts: append(sdk.Coins(nil), reservation.RemainingAmounts...),
	}
}

type storageMessage interface {
	proto.Message
	Marshal() ([]byte, error)
}

// marshalStorage uses generated field-order marshaling. The disk-only messages
// deliberately contain no maps; their only repeated fields are ordered slices,
// so the generated encoding is deterministic. Keeping this helper constrained
// to generated storage messages prevents an accidental generic encoder change.
func marshalStorage(message storageMessage) ([]byte, error) {
	encoded, err := message.Marshal()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), encoded...), nil
}

func normalizeParamsAddresses(params types.Params) (types.Params, error) {
	allowedAddresses, err := decodeAddressStrings(params.AllowedList)
	if err != nil {
		return types.Params{}, fmt.Errorf("invalid legacy allowed-list address: %w", err)
	}
	params.AllowedList, err = encodeAddressBytes(allowedAddresses)
	return params, err
}

func normalizeLeaseAddresses(lease types.Lease) (types.Lease, error) {
	tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
	if err != nil {
		return types.Lease{}, fmt.Errorf("invalid legacy lease tenant address: %w", err)
	}
	lease.Tenant = tenant.String()
	return lease, nil
}

func normalizeCreditAccountAddresses(account types.CreditAccount) (types.CreditAccount, error) {
	tenant, err := sdk.AccAddressFromBech32(account.Tenant)
	if err != nil {
		return types.CreditAccount{}, fmt.Errorf("invalid legacy credit-account tenant address: %w", err)
	}
	creditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
	if err != nil {
		return types.CreditAccount{}, fmt.Errorf("invalid legacy credit address: %w", err)
	}
	account.Tenant = tenant.String()
	account.CreditAddress = creditAddress.String()
	return account, nil
}

func decodeAddressStrings(addresses []string) ([][]byte, error) {
	decoded := make([][]byte, 0, len(addresses))
	for _, address := range addresses {
		addr, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, append([]byte(nil), addr.Bytes()...))
	}
	return decoded, nil
}

func encodeAddressBytes(addresses [][]byte) ([]string, error) {
	encoded := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressString, err := accountAddressString(address)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, addressString)
	}
	return encoded, nil
}

func accountAddressString(address []byte) (string, error) {
	if err := sdk.VerifyAddressFormat(address); err != nil {
		return "", err
	}
	return sdk.AccAddress(address).String(), nil
}
