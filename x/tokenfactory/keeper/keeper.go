package keeper

import (
	"context"
	"fmt"

	"github.com/reecepbcups/manifest/x/tokenfactory/types"

	"cosmossdk.io/log"
	"cosmossdk.io/store/prefix"
	store "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type (
	// IsAdmin is a function signature that checks if an address is an admin.
	IsAdmin func(ctx context.Context, addr string) bool
	// ExtraSudoAllowedCheck is a function signature that checks if an extra sudo is allowed.
	// For Sudo Mints to work, it must be enabled and this function must return true.
	// Error is returned if it's not allowed. If nil, it is allowed.
	ExtraSudoAllowedCheck func(ctx context.Context) error

	Keeper struct {
		cdc      codec.BinaryCodec
		storeKey store.StoreKey

		accountKeeper       types.AccountKeeper
		bankKeeper          types.BankKeeper
		communityPoolKeeper types.CommunityPoolKeeper

		enabledCapabilities []string

		// the address capable of executing a MsgUpdateParams message. Typically, this
		// should be the x/gov module account.
		authority string

		IsAdminFunc               IsAdmin
		ExtraSudoAllowedCheckFunc ExtraSudoAllowedCheck
	}
)

// NewKeeper returns a new instance of the x/tokenfactory keeper
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey store.StoreKey,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	communityPoolKeeper types.CommunityPoolKeeper,
	enabledCapabilities []string,
	authority string,
	// use DefaultIsAdminFunc if you don't have a custom one
	isAdminFunc IsAdmin,
	// use DefaultExtraSudoAllowedCheckFunc if you don't have a custom one
	extraSudoCheck ExtraSudoAllowedCheck,
) Keeper {
	return Keeper{
		cdc:      cdc,
		storeKey: storeKey,

		accountKeeper:       accountKeeper,
		bankKeeper:          bankKeeper,
		communityPoolKeeper: communityPoolKeeper,

		authority: authority,

		enabledCapabilities:       enabledCapabilities,
		IsAdminFunc:               isAdminFunc,
		ExtraSudoAllowedCheckFunc: extraSudoCheck,
	}
}

func DefaultIsAdminFunc(ctx context.Context, addr string) bool {
	return false
}

func DefaultExtraSudoAllowedCheckFunc(ctx context.Context) bool {
	return false
}

// GetAuthority returns the x/mint module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

func (k Keeper) GetEnabledCapabilities() []string {
	return k.enabledCapabilities
}

func (k *Keeper) SetEnabledCapabilities(ctx sdk.Context, newCapabilities []string) {
	k.enabledCapabilities = newCapabilities
}

// Logger returns a logger for the x/tokenfactory module
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetDenomPrefixStore returns the substore for a specific denom
func (k Keeper) GetDenomPrefixStore(ctx sdk.Context, denom string) store.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetDenomPrefixStore(denom))
}

// GetCreatorPrefixStore returns the substore for a specific creator address
func (k Keeper) GetCreatorPrefixStore(ctx sdk.Context, creator string) store.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetCreatorPrefix(creator))
}

// GetCreatorsPrefixStore returns the substore that contains a list of creators
func (k Keeper) GetCreatorsPrefixStore(ctx sdk.Context) store.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetCreatorsPrefix())
}
