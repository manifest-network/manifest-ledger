package helpers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProviderWithdrawableResponseJSON_Unmarshal reproduces the proto3-JSON CLI
// output for `query billing provider-withdrawable`: uint64 fields (lease_count and
// pagination.total) are emitted as JSON strings. Unmarshaling that into the raw
// proto QueryProviderWithdrawableResponse fails on pagination.total (the SDK's
// query.PageResponse.Total has no `,string` tag), which broke ictest-billing when
// the query moved to PageRequest/PageResponse. This pins the custom-struct fix.
func TestProviderWithdrawableResponseJSON_Unmarshal(t *testing.T) {
	blob := `{"amounts":[{"denom":"upwr","amount":"2000"}],"lease_count":"3","pagination":{"next_key":null,"total":"0"}}`

	var res ProviderWithdrawableResponseJSON
	require.NoError(t, json.Unmarshal([]byte(blob), &res))

	require.Equal(t, uint64(3), res.LeaseCount)
	require.Len(t, res.Amounts, 1)
	require.Equal(t, "upwr", res.Amounts[0].Denom)
	require.Equal(t, "2000", res.Amounts[0].Amount.String())
	require.NotNil(t, res.Pagination)
	require.Equal(t, "0", res.Pagination.Total)
}
