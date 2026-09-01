# Release engineering

Manifest Ledger releases are created only by pushing a canonical SemVer tag of
the form `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-prerelease`. Build metadata
(`+...`), leading zeroes, branches, and arbitrary tag names are rejected before
any build starts.

## Release authorization boundary

Before enabling publication, repository administrators must create an active
ruleset covering **all tags** that restricts tag creation, update, deletion,
and bypass to the release-admin group. Restricting only `v*` is insufficient:
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

Protect `main` with a ruleset that requires pull requests, CODEOWNER approval,
stale-approval dismissal, conversation resolution, and the relevant build,
test, simulation, E2E, and CodeQL checks. Restrict bypass to the smallest
break-glass admin set. The checked-in CODEOWNERS file covers consensus modules
and release infrastructure, but it is advisory until the branch ruleset
requires code-owner review.

## Publication contract

The release workflow gates publication on the normal build, unit, simulation,
and end-to-end workflows. It then produces and verifies:

- one statically linked `linux/amd64` binary archive, checksum file, and SPDX
  2.3 SBOM for GitHub Releases;
- a provenance and SBOM attestation for the archive;
- a statically linked `linux/amd64` and `linux/arm64` GHCR image manifest, with
  each target-architecture binary executed during its Buildx build and each
  runtime filesystem scanned for known high/critical OS vulnerabilities before
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

The published container intentionally retains the shell utilities required by
the repository's Starship/interchaintest contract and currently uses the image
default UID (`root`). Treat it as a chain/application image, not as a minimal
distroless boundary. Production orchestrators should apply their normal
read-only filesystem, capability, seccomp/AppArmor, network, and volume
controls. Changing the image UID requires a separately rehearsed volume-
ownership migration and is not part of a consensus software upgrade.

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

The application and release workflows pin Go 1.26.7 as a known-compatible
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
inside the Go 1.26.7 builder.

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
