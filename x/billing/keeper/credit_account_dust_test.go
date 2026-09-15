package keeper_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server/api"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Unsolicited denominations can exhaust legacy complete-result capacity. Keep
// that bounded failure explicit and verify the documented REST escape path.
func TestCreditAccountDustRequiresExplicitPagination(t *testing.T) {
	f, _, _, closed := setupImportedClosedWithdrawal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500)), false)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	creditAddress := types.DeriveCreditAddress(f.TestAccs[0])
	accountBefore, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
	require.NoError(t, err)
	quoteBefore, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: closed.Uuid})
	require.NoError(t, err)
	baseBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom)

	// Use the SDK's real gogo-aware JSON and error handling configuration.
	clientContext := client.Context{}.WithInterfaceRegistry(f.App.AppCodec().InterfaceRegistry())
	mux := api.New(clientContext, log.NewNopLogger(), nil).GRPCGatewayRouter
	require.NoError(t, types.RegisterQueryHandlerServer(f.Ctx, mux, querier))
	endpoint := "/liftedinit/billing/v1/credit/" + closed.Tenant
	get := func(parameters url.Values) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(f.Ctx, http.MethodGet, endpoint+"?"+parameters.Encode(), nil)
		mux.ServeHTTP(response, request)
		return response
	}
	require.Equal(t, http.StatusOK, get(nil).Code)

	// Fund an unrelated sender, then execute the real bank message handler.
	// The tenant does not authorize the incoming transfer to its derived address.
	dust := make(sdk.Coins, int(types.MaxCreditAccountBalanceQueryLimit))
	for i := range dust {
		dust[i] = sdk.NewInt64Coin(fmt.Sprintf("dust%04d", i), 1)
	}
	donor := f.TestAccs[4]
	f.fundAccount(t, donor, dust)
	_, err = bankkeeper.NewMsgServerImpl(f.App.BankKeeper).Send(f.Ctx, &banktypes.MsgSend{
		FromAddress: donor.String(), ToAddress: creditAddress.String(), Amount: dust,
	})
	require.NoError(t, err)
	legacyResponse := get(nil)
	require.Equal(t, http.StatusTooManyRequests, legacyResponse.Code, legacyResponse.Body.String())
	require.Contains(t, legacyResponse.Body.String(), "supply pagination")
	require.Equal(t, baseBalance, f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom))

	// Raw REST supports pagination even when an old generated client cannot
	// encode the new fields. Preserve all balances, including unreserved dust.
	parameters := url.Values{"pagination.limit": {"100"}}
	effectivePageLimit := min(100, int(types.MaxCreditAccountBalanceQueryLimit))
	maximumPages := (len(dust) + effectivePageLimit) / effectivePageLimit
	var received sdk.Coins
	for page := 0; ; page++ {
		require.Less(t, page, maximumPages, "REST cursor traversal must terminate")
		response := get(parameters)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var decoded struct {
			Balances   sdk.Coins `json:"balances"`
			Pagination struct {
				NextKey string `json:"next_key"`
			} `json:"pagination"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
		received = append(received, decoded.Balances...)
		if decoded.Pagination.NextKey == "" {
			break
		}
		parameters.Set("pagination.key", decoded.Pagination.NextKey)
	}
	require.Equal(t, dust.Add(baseBalance), received)
	accountAfter, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
	require.NoError(t, err)
	require.Equal(t, accountBefore, accountAfter, "donations and queries do not alter lease reservations")
	quoteAfter, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: closed.Uuid})
	require.NoError(t, err)
	require.Equal(t, quoteBefore, quoteAfter, "unrelated dust does not alter the lease's payable denominations")
}
