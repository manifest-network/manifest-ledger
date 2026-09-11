package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReservedSingleLabelZones(t *testing.T) {
	for _, zone := range []string{"internal", "local", "test", "run", "example.com"} {
		t.Run(zone, func(t *testing.T) {
			params := DefaultParams()
			params.ReservedDomainSuffixes = []string{"." + zone}
			require.NoError(t, params.Validate())
			require.NoError(t, IsValidFQDN("svc."+zone))
			require.True(t, MatchesReservedSuffix("svc."+zone, params.ReservedDomainSuffixes))
			require.False(t, MatchesReservedSuffix("svc.not"+zone, params.ReservedDomainSuffixes))
		})
	}
	for _, suffix := range []string{".", ".-internal", ".internal-", ".INTERNAL", ".a..b", ".123", ".a_b", ".a/b", ".local."} {
		params := DefaultParams()
		params.ReservedDomainSuffixes = []string{suffix}
		require.Error(t, params.Validate(), suffix)
	}
	// Only reservation syntax expands: a tenant still cannot claim a bare label.
	require.Error(t, IsValidFQDN("internal"))
}
