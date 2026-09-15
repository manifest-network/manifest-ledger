package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// These three message descriptors preserve the wire fields from v2.3.1
// (a6e964a61ca848c3ff10bb8b86da142dba4ccbf3), proto/liftedinit/billing/v1/
// {query,types}.proto. Keep them independent of today's generated descriptors:
// an old client cannot send pagination or interpret a response continuation.
//
//nolint:gosec // Frozen public protobuf schema, not credential material.
const legacyCreditAccountDescriptor = `
name: "billing_credit_account_v2_test.proto"
package: "liftedinit.billing.v1"
syntax: "proto3"
dependency: "cosmos/base/v1beta1/coin.proto"
message_type {
  name: "QueryCreditAccountRequest"
  field { name: "tenant" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
}
message_type {
  name: "QueryCreditAccountResponse"
  field { name: "credit_account" number: 1 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".liftedinit.billing.v1.CreditAccount" }
  field { name: "balances" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".cosmos.base.v1beta1.Coin" }
  field { name: "available_balances" number: 3 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".cosmos.base.v1beta1.Coin" }
}
message_type {
  name: "CreditAccount"
  field { name: "tenant" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
  field { name: "credit_address" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING }
  field { name: "active_lease_count" number: 3 label: LABEL_OPTIONAL type: TYPE_UINT64 }
  field { name: "pending_lease_count" number: 4 label: LABEL_OPTIONAL type: TYPE_UINT64 }
  field { name: "reserved_amounts" number: 5 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".cosmos.base.v1beta1.Coin" }
}
`

func TestQueryCreditAccountLegacyWire(t *testing.T) {
	var file descriptorpb.FileDescriptorProto
	require.NoError(t, prototext.Unmarshal([]byte(legacyCreditAccountDescriptor), &file))
	dependencies := new(protoregistry.Files)
	require.NoError(t, dependencies.RegisterFile((&basev1beta1.Coin{}).ProtoReflect().Descriptor().ParentFile()))
	descriptor, err := protodesc.NewFile(&file, dependencies)
	require.NoError(t, err)
	requestDescriptor := descriptor.Messages().ByName("QueryCreditAccountRequest")
	responseDescriptor := descriptor.Messages().ByName("QueryCreditAccountResponse")
	require.Nil(t, requestDescriptor.Fields().ByNumber(2))
	require.Nil(t, responseDescriptor.Fields().ByNumber(4))

	for _, count := range []int{101, int(types.MaxCreditAccountBalanceQueryLimit), int(types.MaxCreditAccountBalanceQueryLimit) + 1} {
		t.Run(fmt.Sprintf("%d_bank_denominations", count), func(t *testing.T) {
			f := initFixture(t)
			tenant := f.TestAccs[0]
			creditAddr := types.DeriveCreditAddress(tenant)
			coins := make(sdk.Coins, count)
			for i := range coins {
				coins[i] = sdk.NewInt64Coin(fmt.Sprintf("denom%04d", i), 2)
			}
			require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant: tenant.String(), CreditAddress: creditAddr.String(),
			}))
			f.fundAccount(t, creditAddr, coins)

			// Marshal with the old request descriptor, then decode exactly as the
			// current server does. An absent field must remain nil, not PageRequest{}.
			oldRequest := dynamicpb.NewMessage(requestDescriptor)
			oldRequest.Set(requestDescriptor.Fields().ByName("tenant"), protoreflect.ValueOfString(tenant.String()))
			wireRequest, err := proto.Marshal(oldRequest)
			require.NoError(t, err)
			var request types.QueryCreditAccountRequest
			require.NoError(t, request.Unmarshal(wireRequest))
			require.Nil(t, request.Pagination)
			querier := keeper.NewQuerier(f.App.BillingKeeper)
			response, err := querier.CreditAccount(f.Ctx, &request)
			if count > int(types.MaxCreditAccountBalanceQueryLimit) {
				require.Equal(t, codes.ResourceExhausted, status.Code(err))
				require.ErrorContains(t, err, "supply pagination")
				require.Nil(t, response, "old clients must never mistake an incomplete response for all balances")
			} else {
				require.NoError(t, err)
				oldResponse, retainedPagination := decodeLegacyCreditAccountResponse(t, responseDescriptor, response)
				require.Equal(t, query.PageResponse{}, retainedPagination, "a legacy success must not hide continuation in unknown fields")
				for field, expected := range map[protoreflect.Name]sdk.Coins{
					"balances": coins, "available_balances": coins,
				} {
					list := oldResponse.Get(responseDescriptor.Fields().ByName(field)).List()
					require.Equal(t, count, list.Len(), "%s must include every denomination", field)
					for i, coin := range expected {
						actual := list.Get(i).Message()
						require.Equal(t, coin.Denom, actual.Get(actual.Descriptor().Fields().ByName("denom")).String())
						require.Equal(t, coin.Amount.String(), actual.Get(actual.Descriptor().Fields().ByName("amount")).String())
					}
				}
				require.Empty(t, response.Pagination.GetNextKey())
			}

			// A present empty PageRequest opts into cursor paging, even when
			// the legacy complete-result ceiling rejects this same account.
			pageRequest := &query.PageRequest{}
			var paged sdk.Coins
			pageCount := (count + int(types.DefaultCreditAccountBalanceQueryLimit) - 1) / int(types.DefaultCreditAccountBalanceQueryLimit)
			for page := 0; ; page++ {
				require.Less(t, page, pageCount, "cursor traversal must terminate")
				response, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
					Tenant: tenant.String(), Pagination: pageRequest,
				})
				require.NoError(t, err)
				if page == 0 {
					require.Len(t, response.Balances, int(types.DefaultCreditAccountBalanceQueryLimit))
					require.NotEmpty(t, response.Pagination.NextKey)
					// Decode a real partial page with the same old descriptor. This
					// proves the retained-field check detects a nonempty cursor.
					_, retainedPagination := decodeLegacyCreditAccountResponse(t, responseDescriptor, response)
					require.Equal(t, *response.Pagination, retainedPagination)
					require.NotEmpty(t, retainedPagination.NextKey)
				}
				paged = append(paged, response.Balances...)
				if len(response.Pagination.NextKey) == 0 {
					break
				}
				pageRequest = &query.PageRequest{Key: response.Pagination.NextKey}
			}
			require.Equal(t, coins, paged)
		})
	}
}

func decodeLegacyCreditAccountResponse(t *testing.T, descriptor protoreflect.MessageDescriptor, response *types.QueryCreditAccountResponse) (*dynamicpb.Message, query.PageResponse) {
	t.Helper()
	wire, err := response.Marshal()
	require.NoError(t, err)
	oldResponse := dynamicpb.NewMessage(descriptor)
	require.NoError(t, proto.Unmarshal(wire, oldResponse))

	// The old schema cannot interpret field 4, but retaining its wire bytes
	// lets this test inspect the PageResponse that an old client would ignore.
	unknown := oldResponse.GetUnknown()
	number, wireType, tagSize := protowire.ConsumeTag(unknown)
	require.Positive(t, tagSize, "pagination must survive as an unknown field")
	require.Equal(t, protowire.Number(4), number)
	require.Equal(t, protowire.BytesType, wireType)
	encodedPage, valueSize := protowire.ConsumeBytes(unknown[tagSize:])
	require.Positive(t, valueSize)
	require.Empty(t, unknown[tagSize+valueSize:], "unexpected additional response fields")
	var page query.PageResponse
	require.NoError(t, page.Unmarshal(encodedPage))
	return oldResponse, page
}
