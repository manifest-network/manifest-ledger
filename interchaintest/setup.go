package interchaintest

import (
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	poatypes "github.com/strangelove-ventures/poa"
	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"

	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
)

const (
	ExternalGoCoverDir = "/tmp/manifest-ledger-coverage/unit-e2e"
)

var (
	votingPeriod     = "15s"
	maxDepositPeriod = "10s"
	Denom            = "umfx"

	// PoA Admin
	accAddr     = "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	accMnemonic = "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry"

	acc2Addr     = "manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z"
	acc3Addr     = "manifest1sc0aw0e6mcrm7ex5v3ll5gh4dq5whn3acmkupn"
	acc3Mnemonic = "pelican gasp plunge better swallow school infant magic mercy portion candy beauty intact soldier scan must plate logic trial horror theory scrub sorry stand"
	acc4Addr     = "manifest1g292xgmcclhq4au5p7580m2s3f8tpwjra644jm"
	acc4Mnemonic = "myself bamboo retire day exile cancel peanut agree come method odor innocent welcome royal engage key surprise practice capable sauce orient young divert mirror"

	// Taken from https://github.com/manifest-network/manifestjs/blob/main/starship/src/test_helper.ts
	val1Mnemonic = "venture obtain second cricket please sheriff hybrid eyebrow weasel saddle switch abuse artwork clump ivory vault response diary plunge weekend wheat breeze gaze occur"
	val1Addr     = "manifestvaloper19h4chfdz729096nm5hhakc22puwwezgg7mz99x"
	val1Pubkey   = "lnfZyOEx8KQVO1Z3jPxn/03BIPx0Nwu6ApxyEBEWtYM="

	vals      = 2
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
		// Mint - this is the only param the manifest module depends on from mint
		cosmos.NewGenesisKV("app_state.mint.params.blocks_per_year", "6311520"),
		// Block and auth params
		cosmos.NewGenesisKV("consensus.params.block.max_gas", "100000000"), // 100M gas limit
		cosmos.NewGenesisKV("app_state.auth.params.tx_size_cost_per_byte", "1"),
	}

	// `make local-image`
	LocalChainConfig = ibc.ChainConfig{
		Type:    "cosmos",
		Name:    "manifest",
		ChainID: "manifest-2",
		Env: []string{
			fmt.Sprintf("POA_ADMIN_ADDRESS=%s", accAddr),
		},
		Images: []ibc.DockerImage{
			{
				Repository: "manifest",
				Version:    "local",
				UIDGID:     "1025:1025",
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

	DefaultGenesisAmt = sdkmath.NewInt(10_000_000_000_000)
)

func init() {
	sdk.GetConfig().SetBech32PrefixForAccount(LocalChainConfig.Bech32Prefix, LocalChainConfig.Bech32Prefix+"pub")
}

func AppEncoding() *sdktestutil.TestEncodingConfig {
	enc := cosmos.DefaultEncoding()

	manifesttypes.RegisterInterfaces(enc.InterfaceRegistry)
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	tokenfactorytypes.RegisterInterfaces(enc.InterfaceRegistry)
	poatypes.RegisterInterfaces(enc.InterfaceRegistry)
	wasmtypes.RegisterInterfaces(enc.InterfaceRegistry)

	return &enc
}
