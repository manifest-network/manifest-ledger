# Release engineering

Manifest Ledger releases are created only by pushing a canonical SemVer tag of
the form `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-prerelease`. Build metadata
(`+...`), leading zeroes, branches, and arbitrary tag names are rejected before
any build starts.

## Release authorization boundary

Before enabling publication, repository administrators must create an active
ruleset covering **all tags** that restricts tag creation, update, deletion,
and bypass to the designated release maintainer. Restricting only `v*` is insufficient:
an arbitrary tag can execute the workflow from the historical commit it points
to, including older workflows that did not have the current validation gates.
Administrators must also protect the GitHub
`release` environment used by both publication jobs with required reviewers
and a deployment policy limited to protected release tags, and enable GitHub's
**immutable releases** repository setting. Do not push a release tag until all
three controls are active.

These repository controls are part of the release security boundary. A
workflow can prove that a tag is canonical and still points at the triggering
commit, but it cannot prove that the person allowed to create that tag was an
authorized release manager. The workflow intentionally cannot substitute for
a protected tag namespace or environment approval.

As a repository security baseline, also enable Dependabot security updates,
secret scanning, secret-scanning push protection, and the Actions policy that
requires full commit SHA pins. The checked-in weekly Dependabot configuration
keeps routine dependency versions moving, but it does not replace alert-driven
security updates or prevent a release credential from being committed. SHA
enforcement also causes older floating-tag workflows to fail closed if an
arbitrary historical tag is created, but does not replace the all-tags ruleset.

The current sole-maintainer policy names `@fmorency` as the only tag-creation
bypass actor and the required `release` environment reviewer. Self-approval is
allowed; administrator environment bypass is disabled. A separate all-tags
ruleset prevents updates and deletion without any bypass actors. The environment
accepts only tags matching `v*`; canonical SemVer is checked by the workflow.
Repository administrators remain trusted to edit these policies.

`main` requires pull requests, conversation resolution, and the relevant build,
unit, simulation, E2E, coverage, and CodeQL checks with strict up-to-date testing.
The PR/check rules have no bypass actors. A separate update restriction permits
only `@fmorency` to update `main` through a pull request, so other write-capable
collaborators cannot merge without the maintainer. Required review count is zero and
CODEOWNER approval is advisory because the project currently has one maintainer;
requiring independent approval would prevent that maintainer from merging their
own changes. Add independent CODEOWNER approval when another maintainer joins.

The expected live configuration is recorded in
[`.github/release-controls.json`](../.github/release-controls.json). Before
creating a release tag and before approving publication, run:

```sh
python3 scripts/verify-release-controls.py
```

This read-only preflight uses the authenticated `gh` administrator session and
fails on API errors, missing controls, or configuration drift. Ordinary workflow
`GITHUB_TOKEN` permissions cannot inspect all administration endpoints, so this
check is an operator gate, not an automatic workflow assertion. Do not add an
administrative write credential to the build workflow to run it. Server-enforced
tag and environment controls remain the authorization boundary. Policies were
configured and read back on 2026-09-14; the preflight must still be run for each
release.

## Publication contract

The release workflow gates publication on the normal build, unit, simulation,
and end-to-end workflows. It then produces and verifies:

- one statically linked `linux/amd64` binary archive, checksum file, and SPDX
  2.3 SBOM for GitHub Releases;
- a provenance and SBOM attestation for the archive;
- a statically linked `linux/amd64` and `linux/arm64` GHCR image manifest, with
  each target-architecture binary executed during its Buildx build and each
  exported runtime verified to contain only the daemon and CA bundle before
  the exact local OCI layout is promoted to GHCR; and
- provenance and SBOM attestations for the published container manifest.

`UPGRADE_TARGET_RELEASE`, `UPGRADE_SOURCE_RELEASE`,
`UPGRADE_SOURCE_BILLING_VERSION`, and `UPGRADE_SOURCE_SKU_VERSION` in the release
workflow are release-specific metadata. Update them to the planned release and
actual live network baseline before tagging. The workflow refuses any other
stable release line (while allowing prereleases of the configured target),
validates source module versions against the target consensus versions, and
uses them to render the migration path in `## Upgrade Details`.

The downloadable tarball is currently **amd64 only**. ARM64 operators must use
the matching multi-platform GHCR image or build from the release tag. Do not
infer an ARM64 tarball from the general ARM64 runtime support statement.

The published container uses Docker's `production` target: a scratch filesystem
containing the statically linked daemon, CA trust roots, and writable home/tmp
paths. It runs as UID/GID `10001:10001`, uses `/home/manifest/.manifest`, and sets
`manifestd` as its entrypoint. Invoke it with arguments, for example
`docker run --rm IMAGE version`. It contains no shell or package manager.

This changes the image's runtime/volume contract. Before upgrading an existing
root-owned deployment, stop the node and back up its data, provision or migrate
the mounted home to UID/GID `10001:10001`, and rehearse startup and restart with
the production image on a snapshot. Mount the daemon home at
`/home/manifest/.manifest` (or supply an explicit writable `--home`). A read-only
root filesystem also needs writable daemon-home and `/tmp` mounts. Do not change
ownership of a live node's data or assume a consensus migration changes Unix
ownership. Configure capabilities, seccomp/AppArmor, and network access at the
orchestrator boundary.

The default Docker target, explicitly named `starship`, retains the root/tools
contract required by Starship and interchaintest. Build it with
`docker build --target starship .` for development; release publication explicitly
selects `--target production`. The development image is not published under the
production release aliases.

## Non-overwrite guarantees

The workflow checks both GitHub Releases and every paginated GHCR package
version for the exact `vMAJOR.MINOR.PATCH` and normalized `MAJOR.MINOR.PATCH`
tags. Authentication, authorization, pagination, rate-limit, and network errors
fail closed. The check runs before validation work and again immediately before
image publication. The remote tag is also dereferenced through the GitHub API
immediately before image and GitHub Release publication and must still resolve
to the workflow's event commit.

Restrict GHCR package write access to this release workflow and the smallest
possible admin set. The checks above narrow the container-tag race window, but
they cannot make a multi-platform registry push atomic against another
authorized writer. If
GHCR adds an immutable-tag policy for this package, enable it before relying on
tags alone as an atomic non-overwrite boundary; consumers should pin image
digests in the meantime.

## Partial-publication recovery

Publication is deliberately ordered so the GitHub Release is last. If a run
fails before any exact image tag is pushed, fix the cause and rerun it. If the
exact GHCR tag exists but the GitHub Release was not created, a rerun stops at
the collision gate instead of overwriting the image.

Verified release assets are retained for 14 days so protected-environment
review can complete before publication. Reject or rerun an approval that has
outlived that window; do not approve image publication when the archive
artifact is no longer available to the dependent GitHub Release job.

For that partial state:

1. Compare the package digest, attestations, source commit, version metadata,
   and retained workflow artifacts with the failed run.
2. If any value is uncertain, publish a new patch version. Never reuse the tag.
3. If policy permits recovery of the identical verified build, an administrator
   may complete publication manually or remove only that failed package version
   before rerunning. Record the intervention in the release notes/audit trail.

Do not delete or move the Git tag to make a failed release pass.

## Reproducibility boundary

Source actions, base images, tool versions, direct OS packages, wasmvm static
libraries, and downloaded Buildx, Syft, and Trivy bytes are pinned or
checksum-verified. The build also emits provenance, checksums, and SBOMs. This
is a supply-chain integrity contract, not a claim of bit-for-bit
reproducibility: hosted runner images and transitive APK dependency closures
can change. A bit-reproducible release would require a digest-pinned,
pre-provisioned toolchain image or immutable package-repository snapshot
containing the full dependency closure.

The application and release workflows pin Go 1.26.8 as a known-compatible
baseline on the supported 1.26 release line. Before tagging, compare that pin
with the latest Go 1.26 patch and update every module, workflow, and builder pin
when a newer official Alpine image is available. Resolve and review the
published multi-platform registry index digest; never infer it from source or
from a child manifest. Rerun the full build, race, simulation, release-artifact,
and multi-architecture upgrade checks after changing it. Go 1.25.14 contains
the same final `net/http` fix but became unsupported when Go 1.27 shipped; do
not downgrade the release builder to an end-of-life toolchain merely to
minimize the binary diff. GoReleaser 2.18 itself requires Go 1.27 to compile,
but that host-side, checksum-database-verified orchestration binary does not
compile the application; the application build runs with `GOTOOLCHAIN=local`
inside the Go 1.26.8 builder.

The standalone archive is compiled by the Dockerfile's digest-pinned Alpine
builder, not the hosted runner's libc toolchain. The release wrapper disables
module downloads while GoReleaser runs, mounts checksum-verified Syft and
GoReleaser binaries read-only, and uses the builder's pinned patched musl and
wasmvm static libraries.

Image jobs install checksum-verified Buildx into a job-private Docker CLI
configuration, verify that Docker discovers the exact version, and then make
the plugin namespace read-only. An unexpected setup-action fallback therefore
fails before it can replace or execute the verified plugin.

### Native-code advisory boundary

`govulncheck` covers reachable Go code, and the Trivy gates cover runtime APK
packages. Neither is a complete vulnerability scanner for Rust crates compiled
into the prebuilt static wasmvm library. The exact wasmvm release and both
architecture assets are version- and checksum-pinned, and release rehearsals
verify the embedded runtime version, but release owners must also review the
[CosmWasm advisories](https://github.com/CosmWasm/advisories) and wasmvm release
notes before approval. Treat a machine-readable upstream native SBOM or VEX as
a future improvement when wasmvm publishes one; do not describe the current
Go/APK scans as comprehensive native-code coverage.

Dependabot maintains action references, Dockerfile bases, and Go modules, but
does not update workflow input values such as BuildKit, binfmt, and the BuildKit
Syft scanner, or shell-installed Buildx, Syft, Trivy, and wasmvm assets. Review
those pins and their upstream checksums at least before every release (and
monthly during active development); updating a version without its verified
digest or checksum must fail review.

## Local checks

Before pushing a release tag, run at minimum:

```bash
make build
make test
make lint
make govulncheck
make sim-import-export
make sim-after-import
make sim-app-determinism
make sim-full-app
```

The vulnerability policy is fail-closed and every exception is pinned to an
exact module version. Docker Engine daemon advisories are accepted only for the
`interchaintest` profile when the repository imports client code but does not
build or ship the daemon; the release and container profiles remain unaffected.
The E2E and release-publication image gates scan both shipped architecture
filesystems against a fresh Trivy database and do not ignore vulnerabilities
merely because the distribution has not yet published a fix. Release
publication scans filesystem exports from the same multi-output BuildKit solve
that produces the OCI layout; it does not rebuild after the scan.

The simulation targets default to the fixed seed
`SIM_SEED=2507940531156952020`. Keep that seed
for reproducible release evidence; Cosmos SDK treats seed `42` as a sentinel
and the determinism harness replaces it with a process-random seed. Use the
corresponding `-random` targets only for additional randomized coverage.

For an upgrade release, also complete the version-pinned chain-upgrade test and
the migration rehearsal in [`network/manifest-1/UPGRADES.md`](../network/manifest-1/UPGRADES.md).

```bash
make ictest-chain-upgrade-local
```

The local target rebuilds the upgrade image from the working tree and checks
its embedded version and commit. The similarly named target without `-local`
is CI-only and consumes the content-identified image artifact built by the E2E
workflow.

## Compatibility and coverage gates

`make proto-breaking` runs Buf's breaking-change checker against the released
protobuf tree pinned by `.github/protobuf-baseline.ref` (currently `v2.3.1`).
The tag must still resolve to the recorded commit. Advance that baseline to the
new release only after publication; do not move it to a PR head to hide changes.
Wire checks complement the old-descriptor `CreditAccount` tests: protobuf cannot
detect semantic truncation of a successful response.

`codecov.yaml` owns the 80% project and patch **line coverage** floors. Both
Codecov checks remain required. Separately, CI requires 80% Go statement coverage
across the combined profile and 80% changed executable Go statements. The latter
check (`scripts/coverage-diff.py`) reads the complete local Git diff against the PR
base or the previous main commit, without a provider API file limit. Go's parser
and scanner identify logical statements and their own token-bearing lines. A
statement counts once when one of those lines is added; its execution is taken
from the coverage block containing its first token. Nested bodies count
separately. Comments, blank lines, empty statements, and statements whose own
tokens are only on unchanged lines receive no credit. Statements sharing a
changed physical line count together; this is a line-based diff, not a token
diff. Renames count as deletion plus addition.
This AST metric is distinct from Go's block-level `NumStmt` and does not replace
either Codecov line metric. It does not measure branches or individual
expressions: case/communication headers have no independent statement counter,
and package variable initializers outside function bodies are not instrumented
by Go. Package-scope function literal bodies are included. Source-position line
directives are unsupported and fail explicitly.

The combined profile includes `cmd/manifestd/cmd/testnet.go`; generated protobuf
files are excluded. Ordinary `go test -coverprofile` output is merged with runtime
`covdata` output using Go's maintained profile parser; this retains zero-count leaf
packages that binary coverage can omit. Merge hit counts are normalized to zero
or one to avoid double-counting the same test runs. Each invocation cleans a
dedicated binary merge directory, so a previous invocation's counters cannot
supply stale hits. The diff check also excludes Go test
files and independently requires a profile block for every executable statement
in each changed Go file. A missing file or partially missing function fails even
if the remaining measured coverage exceeds 80%. Declaration-only files and empty
bodies need no execution evidence. There are no automatic platform/build-tag
waivers: collect the relevant package/platform profile when it contains changed
executable source. A patch with no changed executable tokens reports N/A only
after completeness validation, without claiming 100% coverage. Billing and SKU
have separate Codecov components, and CI retains the raw combined profile,
per-package Go statement summary, and full-diff AST statement summary, including
when a coverage floor fails.

The `govulncheck` CI job also retains module-level advisory JSON for both Go
modules (`make govulncheck-module-report`). Review this inventory even when the
symbol-reachability gate passes: affected modules can contain unused vulnerable
features. The inventory is evidence for triage, not an assertion that every
module advisory is exploitable in the daemon. Keep reachable findings governed
by the existing narrowly scoped vulnerability policy.

Release artifact verification requires an isolated hash-locked SPDX validator:

```sh
sh scripts/install-spdx-validator.sh /new/path/to/spdx-validator
export SPDX_PYTHON=/new/path/to/spdx-validator/bin/python
```

The verifier runs the official SPDX 2.3 semantic validator and compares the SBOM
with the actual archive and daemon: subject and binary SHA256, main module,
standard-library version, every dependency/version from Go build information
(including module replacements), and the connecting relationships. A valid but
incomplete one-package document fails. CI installs the validator in both artifact
preparation and final publication jobs; the downloaded artifacts are verified
again before attestation.

This verifies the dependency inventory represented by Go build information. It
does not enumerate the internal Rust/C dependency graph of statically linked
WasmVM or musl. Their pinned artifact/base-image checksums and native-binary
checks remain separate controls; do not interpret a passing SPDX check as proof
of a complete native dependency inventory or absence of vulnerabilities.

The production filesystem verifier rejects extra regular files, symlinks, and
special files. Trivy's OS scan still runs on the exported filesystems, but scratch
has no OS package database, so that scan does not inventory statically linked
native libraries. The development images retain APK metadata and their OS
package scans remain applicable. Go reachability scans, native-library checksum
verification, and the documented native advisory review remain separate gates.
