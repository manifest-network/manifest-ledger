package keeper_test

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	manifest "github.com/liftedinit/manifest-ledger/x/manifest"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/stretchr/testify/require"
)

// Call BeginBlocker and make sure values are as expected

const (
	MintDenom = "umfx"
)

func TestStakeholderAutoMint(t *testing.T) {

	// create an account
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(f.Ctx, authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// set the mint keeper params
	defaultParams := minttypes.DefaultParams()
	defaultParams.MintDenom = MintDenom
	defaultParams.InflationMax = sdkmath.LegacyNewDec(1)
	f.App.MintKeeper.Params.Set(f.Ctx, defaultParams)

	sh := []*types.StakeHolders{
		{
			Address:    acc.String(),
			Percentage: 100_000_000,
		},
	}
	_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params:    types.NewParams(sh),
	})
	require.NoError(t, err)

	// mint a bunch of total supply 100_000_000_000 umfx
	f.App.BankKeeper.MintCoins(f.Ctx, "mint", sdk.NewCoins(sdk.NewCoin(MintDenom, sdkmath.NewInt(100_000_000_000))))

	// get balance of acc
	balance := f.App.BankKeeper.GetBalance(f.Ctx, acc, MintDenom)
	require.EqualValues(t, 0, balance.Amount.Int64())

	err = manifest.BeginBlocker(f.Ctx, k, f.App.MintKeeper, f.App.BankKeeper)
	require.NoError(t, err)

	balance = f.App.BankKeeper.GetBalance(f.Ctx, acc, MintDenom)
	require.True(t, balance.Amount.Int64() > 0)

	fmt.Println("balance", balance.Amount.Int64())
}
