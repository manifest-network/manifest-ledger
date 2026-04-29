package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestIsValidFQDN(t *testing.T) {
	cases := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid", "app.example.com", false},
		{"valid_subdomain", "a.b.c.example.test", false},
		{"valid_with_hyphen", "my-app.example.com", false},
		{"valid_max_label", strings.Repeat("a", 63) + ".com", false},
		{"empty", "", true},
		{"too_long", strings.Repeat("a", 254), true},
		{"uppercase", "App.Example.Com", true},
		{"with_scheme", "https://example.com", true},
		{"with_path", "example.com/foo", true},
		{"with_space", "ex ample.com", true},
		{"with_at", "user@example.com", true},
		{"with_wildcard", "*.example.com", true},
		{"with_question", "example.com?x=1", true},
		{"with_hash", "example.com#frag", true},
		{"leading_dot", ".example.com", true},
		{"trailing_dot", "example.com.", true},
		{"no_dot", "localhost", true},
		{"label_too_long", strings.Repeat("a", 64) + ".com", true},
		{"label_leading_hyphen", "-foo.example.com", true},
		{"label_trailing_hyphen", "foo-.example.com", true},
		{"empty_label", "foo..example.com", true},
		{"all_numeric_tld", "192.168.1.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := types.IsValidFQDN(tc.domain)
			if tc.wantErr {
				require.Error(t, err, "expected error for %q", tc.domain)
			} else {
				require.NoError(t, err, "unexpected error for %q: %v", tc.domain, err)
			}
		})
	}
}

func TestMatchesReservedSuffix(t *testing.T) {
	reserved := []string{".barney0.manifest0.net", ".barney8.manifest0.net"}

	cases := []struct {
		name   string
		domain string
		match  bool
	}{
		{"subdomain_match", "app.barney0.manifest0.net", true},
		{"deeper_subdomain_match", "x.y.z.barney0.manifest0.net", true},
		{"apex_match", "barney0.manifest0.net", true},
		{"second_suffix_match", "app.barney8.manifest0.net", true},
		{"case_insensitive", "App.BARNEY0.manifest0.net", true},
		{"non_match_no_boundary", "xbarney0.manifest0.net", false},
		{"non_match_unrelated", "app.example.com", false},
		{"non_match_partial_apex", "manifest0.net", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.match, types.MatchesReservedSuffix(tc.domain, reserved))
		})
	}
}

func TestMatchesReservedSuffix_FailsClosedOnMalformedEntries(t *testing.T) {
	// Entries that should be rejected by Params.Validate (no leading dot, or
	// shorter than 2 characters) are treated as a match — fail-closed for a
	// security check. The matcher is reached only if a malformed entry slipped
	// past validation, in which case refusing the claim is the safe default.
	require.True(t, types.MatchesReservedSuffix("app.barney0.manifest0.net", []string{"barney0.manifest0.net"}))
	require.True(t, types.MatchesReservedSuffix("app.example.com", []string{"."}))
}

func TestParamsValidate_ReservedDomainSuffixes(t *testing.T) {
	base := types.DefaultParams()

	cases := []struct {
		name     string
		suffixes []string
		wantErr  bool
	}{
		{"empty_ok", nil, false},
		{"valid_single", []string{".example.com"}, false},
		{"valid_multi", []string{".a.example.com", ".b.example.com"}, false},
		{"missing_leading_dot", []string{"example.com"}, true},
		{"only_dot", []string{"."}, true},
		{"invalid_fqdn_after_dot", []string{".."}, true},
		{"uppercase", []string{".Example.Com"}, true},
		{"duplicate", []string{".example.com", ".example.com"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.ReservedDomainSuffixes = tc.suffixes
			err := p.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgSetLeaseCustomDomain_ValidateBasic(t *testing.T) {
	const validUUID = "01912345-6789-7abc-8def-0123456789ab"
	_, _, addr := testdata.KeyTestPubAddr()
	validAddr := addr.String()

	cases := []struct {
		name    string
		msg     types.MsgSetLeaseCustomDomain
		wantErr bool
	}{
		{
			name:    "valid_set",
			msg:     types.MsgSetLeaseCustomDomain{Sender: validAddr, LeaseUuid: validUUID, CustomDomain: "app.example.com"},
			wantErr: false,
		},
		{
			name:    "valid_clear",
			msg:     types.MsgSetLeaseCustomDomain{Sender: validAddr, LeaseUuid: validUUID, CustomDomain: ""},
			wantErr: false,
		},
		{
			name:    "invalid_sender",
			msg:     types.MsgSetLeaseCustomDomain{Sender: "not-bech32", LeaseUuid: validUUID, CustomDomain: "x.com"},
			wantErr: true,
		},
		{
			name:    "empty_uuid",
			msg:     types.MsgSetLeaseCustomDomain{Sender: validAddr, LeaseUuid: "", CustomDomain: "x.com"},
			wantErr: true,
		},
		{
			name:    "bad_uuid",
			msg:     types.MsgSetLeaseCustomDomain{Sender: validAddr, LeaseUuid: "not-a-uuid", CustomDomain: "x.com"},
			wantErr: true,
		},
		{
			name:    "bad_fqdn",
			msg:     types.MsgSetLeaseCustomDomain{Sender: validAddr, LeaseUuid: validUUID, CustomDomain: "NoUpper.com"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
