package interchaintest

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/client"
	poatypes "github.com/strangelove-ventures/poa"
	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"
	"github.com/stretchr/testify/require"

	grouptypes "github.com/cosmos/cosmos-sdk/x/group"

	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"

	"github.com/liftedinit/manifest-ledger/x/manifest/types"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
)

const (
	ExternalGoCoverDir = "/tmp/manifest-ledger-coverage"
)

var (
	votingPeriod     = "15s"
	maxDepositPeriod = "10s"
	Denom            = "umfx"

	// PoA Admin
	accAddr     = "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	accMnemonic = "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry"

	acc2Addr = "manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z"

	CosmosGovModuleAcc = "manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm"

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
		// PoA
		cosmos.NewGenesisKV("app_state.poa.params.admins", []string{CosmosGovModuleAcc, accAddr}),
		// Mint - this is the only param the manifest module depends on from mint
		cosmos.NewGenesisKV("app_state.mint.params.blocks_per_year", "6311520"),
		// Manifest
		cosmos.NewGenesisKV("app_state.manifest.params.stake_holders", types.NewStakeHolders(types.NewStakeHolder(acc2Addr, 100_000_000))), // 100% of the inflation payout goes to them
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
				Repository: "manifest-cov",
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

	manifesttypes.RegisterInterfaces(enc.InterfaceRegistry)
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	tokenfactorytypes.RegisterInterfaces(enc.InterfaceRegistry)
	poatypes.RegisterInterfaces(enc.InterfaceRegistry)

	return &enc
}

func CopyCoverageFromContainer(ctx context.Context, t *testing.T, client *client.Client, containerId string, internalGoCoverDir string) {
	r, _, err := client.CopyFromContainer(ctx, containerId, internalGoCoverDir)
	require.NoError(t, err)
	defer r.Close()

	err = os.MkdirAll(ExternalGoCoverDir, os.ModePerm)
	require.NoError(t, err)

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		require.NoError(t, err)

		var fileBuff bytes.Buffer
		_, err = io.Copy(&fileBuff, tr)
		require.NoError(t, err)

		name := hdr.Name
		extractedFileName := path.Base(name)

		//Only extract coverage files
		if !strings.HasPrefix(extractedFileName, "cov") {
			continue
		}
		isDirectory := extractedFileName == ""
		if isDirectory {
			continue
		}

		filePath := filepath.Join(ExternalGoCoverDir, extractedFileName)
		err = os.WriteFile(filePath, fileBuff.Bytes(), os.ModePerm)
		require.NoError(t, err)
	}
}
