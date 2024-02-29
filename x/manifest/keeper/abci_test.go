package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"

	manifest "github.com/liftedinit/manifest-ledger/x/manifest"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

const (
	MintDenom = "umfx"
)

func TestStakeholderAutoMint(t *testing.T) {
	// create an account
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	sh := []*types.StakeHolders{
		{
			Address:    acc.String(),
			Percentage: 100_000_000,
		},
	}
	_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params:    types.NewParams(sh, true, 100_000_000_000, MintDenom),
	})
	require.NoError(t, err)

	balance := f.App.BankKeeper.GetBalance(f.Ctx, acc, MintDenom)
	require.EqualValues(t, 0, balance.Amount.Int64())

	err = manifest.BeginBlocker(f.Ctx, k, f.App.MintKeeper)
	require.NoError(t, err)

	balance = f.App.BankKeeper.GetBalance(f.Ctx, acc, MintDenom)
	require.True(t, balance.Amount.Int64() > 0)
	fmt.Println("balance", balance.Amount.Int64())
}
