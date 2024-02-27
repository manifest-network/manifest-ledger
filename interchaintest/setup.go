package interchaintest

import (
	tokenfactorytypes "github.com/reecepbcups/tokenfactory/x/tokenfactory/types"

	types "github.com/liftedinit/manifest-ledger/x/manifest/types"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
)

var (
	votingPeriod     = "15s"
	maxDepositPeriod = "10s"
	Denom            = "umfx"

	// PoA Admin
	accAddr     = "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	accMnemonic = "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry"

	CosmosGovModuleAcc = "manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm"

	vals      = 1
	fullNodes = 0

	DefaultGenesis = []cosmos.GenesisKV{
		// Governance
		cosmos.NewGenesisKV("app_state.gov.params.voting_period", votingPeriod),
		cosmos.NewGenesisKV("app_state.gov.params.max_deposit_period", maxDepositPeriod),
		// ABCI++
		cosmos.NewGenesisKV("consensus.params.abci.vote_extensions_enable_height", "1"),
		// TokenFactory
		cosmos.NewGenesisKV("app_state.tokenfactory.params.denom_creation_fee", nil),
		cosmos.NewGenesisKV("app_state.tokenfactory.params.denom_creation_gas_consume", "1"),
		// PoA
		cosmos.NewGenesisKV("app_state.poa.params.admins", []string{CosmosGovModuleAcc, accAddr}),
		// Mint - this is the only param the manifest module depends on from mint
		cosmos.NewGenesisKV("app_state.mint.params.blocks_per_year", "6311520"),
		// Manifest
		cosmos.NewGenesisKV("app_state.manifest.params.stake_holders", types.NewStakeHolders(types.NewStakeHolder(accAddr, 100_000_000))), // 100% of the inflation payout goes to them
		cosmos.NewGenesisKV("app_state.manifest.params.inflation.automatic_enabled", true),
		cosmos.NewGenesisKV("app_state.manifest.params.inflation.mint_denom", Denom),
		cosmos.NewGenesisKV("app_state.manifest.params.inflation.yearly_amount", "500000000000"), // in micro denom
	}

	// `make local-image`
	LocalChainConfig = ibc.ChainConfig{
		Type:    "cosmos",
		Name:    "manifest",
		ChainID: "manifest-2",
		Images: []ibc.DockerImage{
			{
				Repository: "manifest",
				Version:    "local",
				UidGid:     "1025:1025",
			},
		},
		Bin:            "manifestd",
		Bech32Prefix:   "manifest",
		Denom:          Denom,
		GasPrices:      "0" + Denom,
		GasAdjustment:  1.3,
		TrustingPeriod: "508h",
		NoHostMount:    false,
		EncodingConfig: AppEncoding(),
		ModifyGenesis:  cosmos.ModifyGenesis(DefaultGenesis),
	}

	DefaultGenesisAmt = sdkmath.NewInt(10_000_000)
)

func AppEncoding() *sdktestutil.TestEncodingConfig {
	enc := cosmos.DefaultEncoding()

	tokenfactorytypes.RegisterInterfaces(enc.InterfaceRegistry)

	return &enc
}
