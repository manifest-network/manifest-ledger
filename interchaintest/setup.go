package interchaintest

import (
	tokenfactorytypes "github.com/reecepbcups/tokenfactory/x/tokenfactory/types"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
)

var (
	votingPeriod     = "15s"
	maxDepositPeriod = "10s"

	// PoA Admin
	accAddr     = "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	accMnemonic = "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry"

	CosmosGovModuleAcc = "manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm"

	vals      = 1
	fullNodes = 0

	DefaultGenesis = []cosmos.GenesisKV{
		cosmos.NewGenesisKV("app_state.gov.params.voting_period", votingPeriod),
		cosmos.NewGenesisKV("app_state.gov.params.max_deposit_period", maxDepositPeriod),
		cosmos.NewGenesisKV("app_state.tokenfactory.params.denom_creation_fee", nil),
		cosmos.NewGenesisKV("app_state.tokenfactory.params.denom_creation_gas_consume", "1"),
		cosmos.NewGenesisKV("consensus.params.abci.vote_extensions_enable_height", "1"),
		cosmos.NewGenesisKV("app_state.poa.params.admins", []string{CosmosGovModuleAcc, accAddr}),
		// inflation of 0 allows for SudoMints. This is enabled by default
		cosmos.NewGenesisKV("app_state.mint.minter.inflation", sdkmath.LegacyZeroDec()),
		cosmos.NewGenesisKV("app_state.mint.params.inflation_rate_change", sdkmath.LegacyZeroDec()), // else it will increase slowly
		cosmos.NewGenesisKV("app_state.mint.params.inflation_min", sdkmath.LegacyZeroDec()),
		// TODO: inflation_max, blocks_per_year?
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
		Denom:          "umfx",
		GasPrices:      "0umfx",
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
