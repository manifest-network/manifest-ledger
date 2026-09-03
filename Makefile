#!/usr/bin/make -f

ifeq ($(origin VERSION), command line)
  $(error VERSION must be passed through the environment, not as a Make command-line variable)
endif
ifeq ($(origin COMMIT), command line)
  $(error COMMIT must be passed through the environment, not as a Make command-line variable)
endif
ifeq ($(origin E2E_IMAGE_VERSION), command line)
  $(error E2E_IMAGE_VERSION must be passed through the environment, not as a Make command-line variable)
endif

# Make expands command-line variable values as Make syntax while constructing
# child environments. Metadata overrides are therefore supported only as
# inherited environment variables, for example:
#
#   VERSION=v2.4.0 COMMIT=0123456789abcdef make build
#
# $(value ...) copies inherited data without expanding it. Only the internal
# names are exported to recipes; the public names are deliberately unexported
# before this file invokes any child process.
unexport MANIFEST_BUILD_COMMIT MANIFEST_BUILD_VERSION MANIFEST_E2E_IMAGE_VERSION
unexport MANIFEST_BUILD_TAGS MANIFEST_WITH_CLEVELDB MANIFEST_LINK_STATICALLY MANIFEST_EXTRA_LDFLAGS
override undefine MANIFEST_BUILD_COMMIT
override undefine MANIFEST_BUILD_VERSION
override undefine MANIFEST_E2E_IMAGE_VERSION
ifneq ($(origin COMMIT), undefined)
  override MANIFEST_BUILD_COMMIT := $(value COMMIT)
endif
ifneq ($(origin VERSION), undefined)
  override MANIFEST_BUILD_VERSION := $(value VERSION)
endif
ifneq ($(origin E2E_IMAGE_VERSION), undefined)
  override MANIFEST_E2E_IMAGE_VERSION := $(value E2E_IMAGE_VERSION)
endif
unexport COMMIT VERSION E2E_IMAGE_VERSION
undefine COMMIT
undefine VERSION
undefine E2E_IMAGE_VERSION

ifeq ($(origin MANIFEST_BUILD_COMMIT), undefined)
  override MANIFEST_BUILD_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || printf '%s' unknown)
endif
ifeq ($(origin MANIFEST_BUILD_VERSION), undefined)
  override MANIFEST_BUILD_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)
endif
ifeq ($(origin MANIFEST_E2E_IMAGE_VERSION), undefined)
  override MANIFEST_E2E_IMAGE_VERSION := $(value MANIFEST_BUILD_VERSION)
endif
export MANIFEST_BUILD_COMMIT MANIFEST_BUILD_VERSION MANIFEST_E2E_IMAGE_VERSION

GO ?= go
PACKAGES_SIMTEST=$(shell $(GO) list ./... | grep '/simulation')
DOCKER := $(shell which docker)
LEDGER_ENABLED ?= true
BINDIR ?= $(GOPATH)/bin
BUILD_DIR = ./build
GOROOT := $(shell $(GO) env GOROOT)
export GOROOT

export GO111MODULE = on

# process build tags

build_tags = netgo
ifeq ($(LEDGER_ENABLED),true)
  ifeq ($(OS),Windows_NT)
    GCCEXE = $(shell where gcc.exe 2> NUL)
    ifeq ($(GCCEXE),)
      $(error gcc.exe not installed for ledger support, please install or set LEDGER_ENABLED=false)
    else
      build_tags += ledger
    endif
  else
    UNAME_S = $(shell uname -s)
    ifeq ($(UNAME_S),OpenBSD)
      $(warning OpenBSD detected, disabling ledger support (https://github.com/cosmos/cosmos-sdk/issues/1988))
    else
      GCC = $(shell command -v gcc 2> /dev/null)
      ifeq ($(GCC),)
        $(error gcc not installed for ledger support, please install or set LEDGER_ENABLED=false)
      else
        build_tags += ledger
      endif
    endif
  endif
endif

ifeq ($(WITH_CLEVELDB),yes)
  build_tags += gcc
endif
build_tags += $(BUILD_TAGS)
build_tags := $(strip $(build_tags))

whitespace :=
empty = $(whitespace) $(whitespace)
comma := ,
build_tags_comma_sep := $(subst $(empty),$(comma),$(build_tags))

override MANIFEST_BUILD_TAGS := $(build_tags_comma_sep)
override MANIFEST_WITH_CLEVELDB := $(value WITH_CLEVELDB)
override MANIFEST_LINK_STATICALLY := $(value LINK_STATICALLY)
override MANIFEST_EXTRA_LDFLAGS := $(value LDFLAGS)
export MANIFEST_BUILD_TAGS MANIFEST_WITH_CLEVELDB MANIFEST_LINK_STATICALLY MANIFEST_EXTRA_LDFLAGS

BUILD_FLAGS = -tags "$${MANIFEST_BUILD_TAGS}" -ldflags "$$(sh ./scripts/build-ldflags.sh)" -trimpath
###########
# Install #
###########

all: install

validate-build-inputs:
	@sh ./scripts/validate-build-inputs.sh metadata

install: validate-build-inputs
	@echo "--> ensure dependencies have not been modified"
	@$(GO) mod verify
	@echo "--> installing manifestd"
	@$(GO) install $(BUILD_FLAGS) -mod=readonly ./cmd/manifestd

init:
	./scripts/init.sh

build: validate-build-inputs
ifeq ($(OS),Windows_NT)
	$(error demo server not supported)
	exit 1
else
	$(GO) build -mod=readonly $(BUILD_FLAGS) -o $(BUILD_DIR)/manifestd ./cmd/manifestd

build-cover: validate-build-inputs
	$(GO) build -mod=readonly $(BUILD_FLAGS) -cover -covermode=atomic -coverpkg=github.com/manifest-network/manifest-ledger/... -o $(BUILD_DIR)/manifestd ./cmd/manifestd
endif

build-vendored: validate-build-inputs
	$(GO) build -mod=vendor $(BUILD_FLAGS) -o $(BUILD_DIR)/manifestd ./cmd/manifestd

.PHONY: all build build-cover build-linux install init lint build-vendored validate-build-inputs

###############################################################################
###                          INTERCHAINTEST (ictest)                        ###
###############################################################################

ictest-ibc:
	cd interchaintest && $(GO) test -race -v -run TestIBC . -count=1

ictest-tokenfactory:
	cd interchaintest && $(GO) test -race -v -run TestTokenFactory . -count=1

ictest-manifest:
	cd interchaintest && $(GO) test -race -v -run TestManifestModule . -count=1

ictest-poa:
	cd interchaintest && $(GO) test -race -v -run TestPOA . -count=1

ictest-poa-unjail-dup:
	cd interchaintest && $(GO) test -timeout 25m -race -v -run TestPOAUnjailDup . -count=1

ictest-poa-unjail-dup-bug:
	cd interchaintest && UNJAIL_DUP_IMAGE=ghcr.io/manifest-network/manifest-ledger:2.1.1 $(GO) test -timeout 25m -race -v -run TestPOAUnjailDup . -count=1

ictest-group-poa:
	cd interchaintest && $(GO) test -timeout 25m -race -v -run TestGroupPOA . -count=1

ictest-cosmwasm:
	cd interchaintest && $(GO) test -race -v -run TestCosmWasm . -count=1

define verify_chain_upgrade_image
$(DOCKER) run --rm \
		--env "EXPECTED_VERSION=$${MANIFEST_E2E_IMAGE_VERSION}" \
		--env "EXPECTED_COMMIT=$${MANIFEST_BUILD_COMMIT}" \
		manifest:local sh -ec '\
			metadata="$$(manifestd version --long --output json)"; \
			actual_version="$$(printf "%s\n" "$$metadata" | jq -er .version)"; \
			actual_commit="$$(printf "%s\n" "$$metadata" | jq -er .commit)"; \
			if [ "$$actual_version" != "$$EXPECTED_VERSION" ] || [ "$$actual_commit" != "$$EXPECTED_COMMIT" ]; then \
				printf "%s\n" \
					"manifest:local identity mismatch:" \
					"  version: $$actual_version (expected $$EXPECTED_VERSION)" \
					"  commit:  $$actual_commit (expected $$EXPECTED_COMMIT)" >&2; \
				exit 1; \
			fi'
endef

verify-chain-upgrade-image: validate-build-inputs
	@$(verify_chain_upgrade_image)

# CI consumes the image artifact built from the same checked-out commit. Local
# runs must use ictest-chain-upgrade-local so dirty source changes cannot be
# hidden by a stale manifest:local image carrying otherwise matching metadata.
ictest-chain-upgrade: validate-build-inputs
	@test "$${CI:-}" = "true" || \
		(printf '%s\n' "local upgrade rehearsals must use: make ictest-chain-upgrade-local"; exit 1)
	@$(verify_chain_upgrade_image)
	cd interchaintest && MANIFEST_UPGRADE_VERSION="$${MANIFEST_E2E_IMAGE_VERSION}" $(GO) test -timeout 20m -race -v -run TestBasicManifestUpgrade . -count=1

ictest-chain-upgrade-local: local-image
	@$(verify_chain_upgrade_image)
	cd interchaintest && MANIFEST_UPGRADE_VERSION="$${MANIFEST_E2E_IMAGE_VERSION}" $(GO) test -timeout 20m -race -v -run TestBasicManifestUpgrade . -count=1

ictest-group:
	cd interchaintest && $(GO) test -race -v -run TestGroupMetadataLimits . -count=1

ictest-sku:
	cd interchaintest && $(GO) test -timeout 20m -race -v -run TestSKU . -count=1

# Full local aggregate. CI runs these suites as separate matrix jobs so each retains
# an independent 45m hang guard.
ictest-billing:
	cd interchaintest && $(GO) test -race -v -timeout 60m -run "^TestBilling(Lease|Credit|Advanced|State|Reservation)$$" . -count=1

# Extra billing e2e tests run as their own parallel CI job.
ictest-billing-extra:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run "^TestBilling(AcknowledgeActiveCap|CustomDomain)$$" . -count=1

ictest-billing-lease:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingLease . -count=1

ictest-billing-credit:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingCredit . -count=1

ictest-billing-advanced:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingAdvanced . -count=1

ictest-billing-state:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingState . -count=1

ictest-billing-upgrade:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingModuleUpgrade . -count=1

ictest-billing-reservation:
	cd interchaintest && $(GO) test -race -v -timeout 45m -run TestBillingReservation . -count=1

.PHONY: ictest-ibc ictest-tokenfactory ictest-manifest ictest-poa ictest-poa-unjail-dup ictest-poa-unjail-dup-bug ictest-group-poa ictest-cosmwasm verify-chain-upgrade-image ictest-chain-upgrade ictest-chain-upgrade-local ictest-group ictest-sku ictest-billing ictest-billing-extra ictest-billing-lease ictest-billing-credit ictest-billing-advanced ictest-billing-state ictest-billing-upgrade ictest-billing-reservation

###############################################################################
###                                Build Image                              ###
###############################################################################

local-image: validate-build-inputs
	@echo "--> Building local image"
	$(DOCKER) build --build-arg BUILD_CMD=build --build-arg "COMMIT=$${MANIFEST_BUILD_COMMIT}" --build-arg "VERSION=$${MANIFEST_E2E_IMAGE_VERSION}" . -t manifest:local

local-image-cover: validate-build-inputs
	@echo "--> Building coverage-instrumented local image"
	$(DOCKER) build --build-arg BUILD_CMD=build-cover --build-arg "COMMIT=$${MANIFEST_BUILD_COMMIT}" --build-arg "VERSION=$${MANIFEST_E2E_IMAGE_VERSION}" . -t manifest:local

.PHONY: local-image local-image-cover

#################
###   Test    ###
#################

test:
	@echo "--> Running tests"
	$(GO) test -v ./...

.PHONY: test

COV_ROOT := /tmp/manifest-ledger-coverage
COV_UNIT_E2E := ${COV_ROOT}/unit-e2e
COV_SIMULATION := ${COV_ROOT}/simulation
COV_PKG := github.com/manifest-network/manifest-ledger/...
COV_SIM_CMD := ${COV_SIMULATION}/simulation.test
# Race and coverage instrumentation make the sequential interchaintest package
# exceed its normal 90-minute runtime as the suite grows.
COV_TEST_TIMEOUT := 2h
# Coverage is a reproducible release gate. Seed exploration belongs to the
# explicit sim-*-random targets below, which always print the selected seed.
COV_SIM_COMMON = -Enabled=True -NumBlocks=100 -Commit=true -Period=5 -Params=$(CURDIR)/simulation/sim_params.json -Verbose=false -Seed=${SIM_SEED} -test.v -test.gocoverdir=${COV_SIMULATION}

define run_coverage_simulation
	@log_file=${COV_ROOT}/$(1).log; \
		${COV_SIM_CMD} -test.run $(2) ${COV_SIM_COMMON} > "$$log_file" 2>&1 || { \
			status=$$?; \
			printf '%s\n' "$(2) failed with seed ${SIM_SEED}; full output follows:" >&2; \
			cat "$$log_file" >&2; \
			exit $$status; \
		}; \
		rm -f "$$log_file"
endef

coverage: ## Run coverage report
	@echo "--> Using Go: $(shell $(GO) version)"
	@echo "--> GOROOT: $(GOROOT)"

	@echo "--> Creating GOCOVERDIR"
	@mkdir -p ${COV_UNIT_E2E} ${COV_SIMULATION}
	@echo "--> Cleaning up coverage files, if any"
	@rm -rf ${COV_UNIT_E2E}/* ${COV_SIMULATION}/*
	@echo "--> Building instrumented simulation test binary"
	@$(GO) test -c ./app -mod=readonly -covermode=atomic -coverpkg=${COV_PKG} -cover -o ${COV_SIM_CMD}
	@echo "  --> Running Full App Simulation (seed: ${SIM_SEED})"
	$(call run_coverage_simulation,full-app,TestFullAppSimulation)
	@echo "  --> Running App Simulation After Import (seed: ${SIM_SEED})"
	$(call run_coverage_simulation,after-import,TestAppSimulationAfterImport)
	@echo "  --> Running App State Determinism Simulation (seed: ${SIM_SEED})"
	$(call run_coverage_simulation,determinism,TestAppStateDeterminism)
	@echo "--> Running unit & e2e tests coverage"
	@$(GO) test -p 1 -timeout ${COV_TEST_TIMEOUT} -race -covermode=atomic -v -cpu=$$(nproc) -cover $$($(GO) list ./...) ./interchaintest/... -coverpkg=${COV_PKG} -args -test.gocoverdir="${COV_UNIT_E2E}"
	@echo "--> Merging coverage reports"
	@$(GO) tool covdata merge -i=${COV_UNIT_E2E},${COV_SIMULATION} -o ${COV_ROOT}
	@echo "--> Converting binary coverage report to text format"
	@$(GO) tool covdata textfmt -i=${COV_ROOT} -o ${COV_ROOT}/coverage-merged.out
	@echo "--> Filtering coverage reports"
	@./scripts/filter-coverage.sh ${COV_ROOT}/coverage-merged.out ${COV_ROOT}/coverage-merged-filtered.out
	@echo "--> Generating coverage report"
	@$(GO) tool cover -func=${COV_ROOT}/coverage-merged-filtered.out
	@echo "--> Generating HTML coverage report"
	@$(GO) tool cover -html=${COV_ROOT}/coverage-merged-filtered.out -o coverage.html
	@echo "--> Coverage report available at coverage.html"
	@echo "--> Cleaning up coverage files"
	@rm -rf ${COV_UNIT_E2E}/* ${COV_SIMULATION}/*
	@echo "--> Running coverage complete"

.PHONY: coverage


##################
###  Protobuf  ###
##################

protoVer=0.14.0
protoDigest=sha256:93e2035b90e5780b4d56210a88ecb0afed881c7bb828285d4a61a897cebb54fb
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)@$(protoDigest)
protoImage=$(DOCKER) run --rm --user "$$(id -u):$$(id -g)" --env HOME=/tmp -v $(CURDIR):/workspace --workdir /workspace $(protoImageName)

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Generating protobuf files..."
	@$(protoImage) sh ./scripts/protocgen.sh
	@$(GO) mod tidy

proto-format:
	@$(protoImage) find ./ -name "*.proto" -exec clang-format -i {} \;

proto-lint:
	@$(protoImage) buf lint proto/ --error-format=json

.PHONY: proto-all proto-gen proto-format proto-lint

#################
###  Linting  ###
#################

golangci_version=v2.12.2
go_bin=$(shell $(GO) env GOPATH)/bin
golangci_lint_cmd=$(go_bin)/golangci-lint

lint:
	@echo "--> Running linter"
	@GOBIN=$(go_bin) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(golangci_version)
	@$(golangci_lint_cmd) run ./... --timeout 15m
	@cd interchaintest && $(golangci_lint_cmd) run ./... --timeout 15m

lint-fix:
	@echo "--> Running linter and fixing issues"
	@GOBIN=$(go_bin) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(golangci_version)
	@$(golangci_lint_cmd) run ./... --fix --timeout 15m
	@cd interchaintest && $(golangci_lint_cmd) run ./... --fix --timeout 15m

.PHONY: lint lint-fix

#### FORMAT ####
goimports_version=v0.49.0

format-install:
	@echo "--> Installing goimports $(goimports_version)"
	@GOBIN=$(go_bin) $(GO) install golang.org/x/tools/cmd/goimports@$(goimports_version)
	@echo "--> Installing goimports $(goimports_version) complete"

format: ## Run formatter (goimports)
	@echo "--> Running goimports"
	$(MAKE) format-install
	@find . -name '*.go' -not -name '*.pulsar.go' -not -name '*.pb.go' -exec $(go_bin)/goimports -w -local github.com/manifest-network/manifest-ledger {} \;

#### GOVULNCHECK ####
govulncheck_version=v1.7.0

govulncheck-install:
	@echo "--> Installing govulncheck $(govulncheck_version)"
	@GOBIN=$(go_bin) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(govulncheck_version)
	@echo "--> Installing govulncheck $(govulncheck_version) complete"

govulncheck: ## Run govulncheck
	@echo "--> Running govulncheck for the static ledger release build"
	$(MAKE) govulncheck-install
	@$(GO) run ./tools/govulncheck-policy -govulncheck $(go_bin)/govulncheck -goos linux -goarch amd64 -- -tags=netgo,muslc,ledger ./...
	@echo "--> Running govulncheck for the linux/amd64 muslc container build"
	@$(GO) run ./tools/govulncheck-policy -govulncheck $(go_bin)/govulncheck -goos linux -goarch amd64 -- -tags=netgo,muslc ./...
	@echo "--> Running govulncheck for the interchaintest module"
	@$(GO) run ./tools/govulncheck-policy -govulncheck $(go_bin)/govulncheck -profile interchaintest -- -test ./interchaintest/...

.PHONY: govulncheck govulncheck-install

#### VET ####

# Pulsar output is generator-owned and currently contains deliberate trailing
# panics after exhaustive switches that Go's unreachable analyzer rejects.
vet: ## Run go vet
	@echo "--> Running go vet"
	@packages="$$($(GO) list ./...)" || exit $$?; \
		packages="$$(printf '%s\n' "$$packages" | grep -v '/api/')"; \
		test -n "$$packages"; \
		$(GO) vet $$packages

.PHONY: vet

#### Simulation ####

SIM_PARAMS ?= $(shell pwd)/simulation/sim_params.json
SIM_NUM_BLOCKS ?= 100
SIM_PERIOD ?= 5
SIM_COMMIT ?= true
SIM_ENABLED ?= true
SIM_VERBOSE ?= false
SIM_TIMEOUT ?= 24h
# Cosmos SDK reserves 42 as DefaultSeedValue, and the determinism harness
# replaces that sentinel with a process-random seed. Keep release simulations
# reproducible across invocations by using an explicit non-sentinel default.
SIM_SEED ?= 2507940531156952020
# $RANDOM is a Bash extension, while Make recipes use /bin/sh. Generate random
# target seeds portably from the OS entropy source instead.
SIM_RANDOM_SEED = $(strip $(shell od -An -N4 -tu4 /dev/urandom))
SIM_COMMON_ARGS = -NumBlocks=${SIM_NUM_BLOCKS} -Enabled=${SIM_ENABLED} -Commit=${SIM_COMMIT} -Period=${SIM_PERIOD} -Params=${SIM_PARAMS} -Verbose=${SIM_VERBOSE} -Seed=${SIM_SEED} -v -timeout ${SIM_TIMEOUT}

sim-full-app:
	@echo "--> Running full app simulation (blocks: ${SIM_NUM_BLOCKS}, commit: ${SIM_COMMIT}, period: ${SIM_PERIOD}, seed: ${SIM_SEED}, params: ${SIM_PARAMS}"
	@$(GO) test ./app -run TestFullAppSimulation ${SIM_COMMON_ARGS}

sim-full-app-random:
	$(MAKE) sim-full-app SIM_SEED=$(SIM_RANDOM_SEED)

sim-import-export:
	@echo "--> Running app import/export simulation (blocks: ${SIM_NUM_BLOCKS}, commit: ${SIM_COMMIT}, period: ${SIM_PERIOD}, seed: ${SIM_SEED}, params: ${SIM_PARAMS}"
	@$(GO) test ./app -run TestAppImportExport ${SIM_COMMON_ARGS}

sim-import-export-random:
	$(MAKE) sim-import-export SIM_SEED=$(SIM_RANDOM_SEED)

sim-after-import:
	@echo "--> Running app after import simulation (blocks: ${SIM_NUM_BLOCKS}, commit: ${SIM_COMMIT}, period: ${SIM_PERIOD}, seed: ${SIM_SEED}, params: ${SIM_PARAMS}"
	@$(GO) test ./app -run TestAppSimulationAfterImport ${SIM_COMMON_ARGS}

sim-after-import-random:
	$(MAKE) sim-after-import SIM_SEED=$(SIM_RANDOM_SEED)

sim-app-determinism:
	@echo "--> Running app determinism simulation (blocks: ${SIM_NUM_BLOCKS}, commit: ${SIM_COMMIT}, period: ${SIM_PERIOD}, seed: ${SIM_SEED}, params: ${SIM_PARAMS}"
	@$(GO) test ./app -run TestAppStateDeterminism ${SIM_COMMON_ARGS}

sim-app-determinism-random:
	$(MAKE) sim-app-determinism SIM_SEED=$(SIM_RANDOM_SEED)

.PHONY: sim-full-app sim-full-app-random sim-import-export sim-after-import sim-app-determinism sim-import-export-random sim-after-import-random sim-app-determinism-random
