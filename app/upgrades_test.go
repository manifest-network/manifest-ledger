package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHaltRecoveryUpgradeHandler guards the 2026-06-04 mainnet recovery failure:
// a halt-recovery binary swap must still register a handler for the chain's last
// applied on-chain upgrade ("v2.1.1"), or x/upgrade's PreBlocker downgrade check
// refuses to boot ("upgrade handler is missing for v2.1.1 upgrade plan"). The
// handler name normally tracks app.Version(), which differs from the last applied
// upgrade on a version-bumping hotfix, so it must be registered explicitly. The
// governance-upgrade e2e test does NOT catch this (there the last applied upgrade
// equals the new binary version).
func TestHaltRecoveryUpgradeHandler(t *testing.T) {
	_, app := Setup(t)
	require.True(t, app.UpgradeKeeper.HasHandler("v2.1.1"),
		"binary must register the v2.1.1 upgrade handler to pass x/upgrade's downgrade check on a binary-swap recovery")
}
