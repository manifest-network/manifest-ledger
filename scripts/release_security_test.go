package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChecksumManifestBindsExactReleaseArtifactSet(t *testing.T) {
	testDir := t.TempDir()
	archive := filepath.Join(testDir, "manifest-ledger-v2.4.0-linux-amd64.tar.gz")
	sbom := archive + ".sbom.json"
	require.NoError(t, os.WriteFile(archive, []byte("archive"), 0o600))
	require.NoError(t, os.WriteFile(sbom, []byte("sbom"), 0o600))

	hash := strings.Repeat("a", 64)
	archiveEntry := fmt.Sprintf("%s  %s\n", hash, filepath.Base(archive))
	sbomEntry := fmt.Sprintf("%s  %s\n", hash, filepath.Base(sbom))
	tests := []struct {
		name      string
		manifest  string
		wantError bool
	}{
		{name: "exact artifact set", manifest: archiveEntry + sbomEntry},
		{name: "missing archive", manifest: sbomEntry, wantError: true},
		{name: "missing SBOM", manifest: archiveEntry, wantError: true},
		{name: "duplicate archive", manifest: archiveEntry + archiveEntry + sbomEntry, wantError: true},
		{name: "pathful artifact", manifest: archiveEntry + fmt.Sprintf("%s  dist/%s\n", hash, filepath.Base(sbom)), wantError: true},
		{name: "unexpected artifact", manifest: archiveEntry + sbomEntry + fmt.Sprintf("%s  extra.txt\n", hash), wantError: true},
		{name: "malformed hash", manifest: "not-a-checksum  " + filepath.Base(archive) + "\n" + sbomEntry, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := filepath.Join(t.TempDir(), "checksums.txt")
			require.NoError(t, os.WriteFile(manifest, []byte(test.manifest), 0o600))
			cmd := exec.Command("sh", "./verify-checksum-manifest.sh", manifest, archive, sbom) //nolint:gosec
			cmd.Dir = "."
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
			output, err := cmd.CombinedOutput()
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err, string(output))
			}
		})
	}
}

func TestContainerizedGoReleaserUsesPinnedOfflineToolchain(t *testing.T) {
	testDir := t.TempDir()
	dockerLog := filepath.Join(testDir, "docker-arguments.txt")
	fakeDocker := filepath.Join(testDir, "docker")
	const fakeDockerSource = `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$FAKE_DOCKER_LOG"
`
	require.NoError(t, os.WriteFile(fakeDocker, []byte(fakeDockerSource), 0o700)) //nolint:gosec

	syft := filepath.Join(testDir, "syft")
	goreleaser := filepath.Join(testDir, "goreleaser")
	require.NoError(t, os.WriteFile(syft, []byte("tool"), 0o600))
	require.NoError(t, os.Chmod(syft, 0o500)) //nolint:gosec // Owner execution is required by the wrapper fixture.
	require.NoError(t, os.WriteFile(goreleaser, []byte("tool"), 0o600))
	require.NoError(t, os.Chmod(goreleaser, 0o500)) //nolint:gosec // Owner execution is required by the wrapper fixture.

	cmd := exec.Command( //nolint:gosec
		"sh", "./run-goreleaser-in-container.sh",
		"manifest-release-builder:test", syft, goreleaser,
		"release", "--snapshot", "--clean", "--skip=publish",
	)
	cmd.Dir = "."
	cmd.Env = []string{
		"PATH=" + testDir + ":" + os.Getenv("PATH"),
		"LC_ALL=C",
		"FAKE_DOCKER_LOG=" + dockerLog,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	arguments, err := os.ReadFile(dockerLog) //nolint:gosec
	require.NoError(t, err)
	invocation := string(arguments)
	require.Contains(t, invocation, "GOPROXY=off\n")
	require.Contains(t, invocation, "GOTOOLCHAIN=local\n")
	require.Contains(t, invocation, "GOWORK=off\n")
	require.Contains(t, invocation, syft+":/usr/local/bin/syft:ro\n")
	require.Contains(t, invocation, goreleaser+":/usr/local/bin/goreleaser:ro\n")
	require.Contains(t, invocation, "manifest-release-builder:test\n")
	require.Contains(t, invocation, "release\n--snapshot\n--clean\n--skip=publish\n")
}

func TestReleaseCollisionCheckFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		releaseTags string
		packageTags string
		failAPI     string
		wantError   bool
	}{
		{name: "no collision"},
		{name: "similar tags are not collisions", releaseTags: "v2.4.00\nv2.4.0-rc.1", packageTags: "2.40.0"},
		{name: "prefixed release collision", releaseTags: "v2.4.0", wantError: true},
		{name: "normalized release collision", releaseTags: "2.4.0", wantError: true},
		{name: "prefixed package collision", packageTags: "v2.4.0", wantError: true},
		{name: "normalized package collision", packageTags: "2.4.0", wantError: true},
		{name: "release API failure", failAPI: "releases", wantError: true},
		{name: "package API failure", failAPI: "packages", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runReleaseCollisionCheck(t, tt.releaseTags, tt.packageTags, tt.failAPI)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestReleaseWorkflowsUseVerifiedToolsAndCollisionGate(t *testing.T) {
	repoRoot := filepath.Clean("..")
	releaseGuide, err := os.ReadFile(filepath.Join(repoRoot, "docs/RELEASE.md")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(releaseGuide), "ruleset covering **all tags**")
	require.Contains(t, string(releaseGuide), "historical commit")

	codeowners, err := os.ReadFile(filepath.Join(repoRoot, ".github/CODEOWNERS")) //nolint:gosec
	require.NoError(t, err)
	for _, protectedPath := range []string{
		"/.github/workflows/ @fmorency",
		"/.dockerignore @fmorency",
		"/Dockerfile @fmorency",
		"/app/ @fmorency",
		"/tools/ @fmorency",
		"/x/billing/ @fmorency",
		"/x/sku/ @fmorency",
	} {
		require.Contains(t, string(codeowners), protectedPath)
	}

	for _, workflowPath := range []string{
		".github/workflows/build.yml",
		".github/workflows/release-bin.yaml",
	} {
		workflow, err := os.ReadFile(filepath.Join(repoRoot, workflowPath)) //nolint:gosec
		require.NoError(t, err)
		require.Contains(t, string(workflow), "sh ./scripts/install-syft.sh")
		require.NotContains(t, string(workflow), "download-syft")
		require.Contains(t, string(workflow), `SYFT_CHECK_FOR_APP_UPDATE: "false"`)
		require.Contains(t, string(workflow), "./scripts/run-goreleaser-in-container.sh")
		require.Contains(t, string(workflow), `CGO_ENABLED: "0"`)
		require.NotContains(t, string(workflow), "musl-tools=")
	}

	buildWorkflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/build.yml")) //nolint:gosec
	require.NoError(t, err)
	protobufJob := workflowJobBlock(t, string(buildWorkflow), "protobuf")
	require.Contains(t, protobufJob, "make proto-all")
	require.Contains(t, protobufJob, "git status --porcelain")

	releaseWorkflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/release-bin.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(releaseWorkflow), "packages: read")
	require.Equal(t, 2, strings.Count(string(releaseWorkflow), "sh ./scripts/check-release-collisions.sh"))
	require.Equal(t, 3, strings.Count(string(releaseWorkflow), "sh ./scripts/verify-release-tag-target.sh"))
	require.Contains(t, string(releaseWorkflow), "go run ./tools/release-notes")
	require.Contains(t, string(releaseWorkflow), `--notes-file "$RELEASE_NOTES"`)
	require.Contains(t, string(releaseWorkflow), `UPGRADE_TARGET_RELEASE: v2.4.0`)
	require.Contains(t, string(releaseWorkflow), `UPGRADE_SOURCE_RELEASE: v2.3.1`)
	require.Contains(t, string(releaseWorkflow), "retention-days: 14")
	require.Equal(t, 2, strings.Count(string(releaseWorkflow), "    environment: release\n"))
	prepareStart := strings.Index(string(releaseWorkflow), "  prepare:\n")
	publishImageStart := strings.Index(string(releaseWorkflow), "  publish-image:\n")
	require.GreaterOrEqual(t, prepareStart, 0)
	require.Greater(t, publishImageStart, prepareStart)
	prepareJob := string(releaseWorkflow)[prepareStart:publishImageStart]
	require.NotContains(t, prepareJob, "id-token: write")
	require.NotContains(t, prepareJob, "attestations: write")
	require.Contains(t, string(releaseWorkflow), "sbom: generator=docker.io/docker/buildkit-syft-scanner:1.12.0@sha256:ae4f3b554449e7e25548e7d8ccc029d17357348e30c6e3df01b92bc93654d6a9")
	require.NotContains(t, string(releaseWorkflow), "sbom: true")
	require.NotContains(t, string(releaseWorkflow), "buildkit-syft-scanner:stable-1")
	require.Contains(t, string(releaseWorkflow), "type=oci,dest=${{ runner.temp }}/manifest-release-oci,tar=false")
	require.Contains(t, string(releaseWorkflow), "type=local,dest=${{ runner.temp }}/manifest-release-rootfs,platform-split=true")
	require.Contains(t, string(releaseWorkflow), `scan_rootfs "$IMAGE_ROOTFS/linux_amd64"`)
	require.Contains(t, string(releaseWorkflow), `scan_rootfs "$IMAGE_ROOTFS/linux_arm64"`)
	require.Contains(t, string(releaseWorkflow), `args+=("oci-layout://${OCI_LAYOUT}@${IMAGE_DIGEST}")`)
	require.Contains(t, string(releaseWorkflow), "subject-digest: ${{ steps.publish.outputs.digest }}")
	require.NotContains(t, string(releaseWorkflow), "\n          push: true\n")
	require.NotContains(t, string(releaseWorkflow), "aquasecurity/trivy-action")
	publishImageJob := workflowJobBlock(t, string(releaseWorkflow), "publish-image")
	require.Contains(t, publishImageJob, `docker_config="$RUNNER_TEMP/docker-config"`)
	require.Contains(t, publishImageJob, `sh ./scripts/install-buildx.sh "$docker_config/cli-plugins"`)
	require.Contains(t, publishImageJob, `DOCKER_CONFIG="$docker_config" docker buildx version | grep --fixed-strings ' v0.36.1 '`)
	require.Contains(t, publishImageJob, `chmod 0555 "$docker_config/cli-plugins/docker-buildx" "$docker_config/cli-plugins"`)
	require.Contains(t, publishImageJob, `"DOCKER_CONFIG=$docker_config" >> "$GITHUB_ENV"`)
	require.NotContains(t, publishImageJob, "\n          version:")
	require.Less(t,
		strings.Index(publishImageJob, "Install checksum-verified Buildx"),
		strings.Index(publishImageJob, "Set up Docker Buildx"),
	)

	e2eWorkflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/e2e.yml")) //nolint:gosec
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(e2eWorkflow), `sh ./scripts/install-trivy.sh "$TRIVY_INSTALL_DIR"`))
	require.Equal(t, 2, strings.Count(string(e2eWorkflow), "--pkg-types os"))
	require.Equal(t, 2, strings.Count(string(e2eWorkflow), "--ignore-unfixed=false"))
	require.Equal(t, 2, strings.Count(string(e2eWorkflow), "--exit-code 1"))
	require.NotContains(t, string(e2eWorkflow), "aquasecurity/trivy-action")
	for _, job := range []string{"build-docker", "build-docker-arm64"} {
		jobBlock := workflowJobBlock(t, string(e2eWorkflow), job)
		require.Contains(t, jobBlock, `docker_config="$RUNNER_TEMP/docker-config"`)
		require.Contains(t, jobBlock, `sh ./scripts/install-buildx.sh "$docker_config/cli-plugins"`)
		require.Contains(t, jobBlock, `DOCKER_CONFIG="$docker_config" docker buildx version | grep --fixed-strings ' v0.36.1 '`)
		require.Contains(t, jobBlock, `chmod 0555 "$docker_config/cli-plugins/docker-buildx" "$docker_config/cli-plugins"`)
		require.Contains(t, jobBlock, `"DOCKER_CONFIG=$docker_config" >> "$GITHUB_ENV"`)
		require.NotContains(t, jobBlock, "\n          version:")
		require.Less(t,
			strings.Index(jobBlock, "Install checksum-verified Buildx"),
			strings.Index(jobBlock, "Set up Docker Buildx"),
		)
	}
	e2eTestsJob := workflowJobBlock(t, string(e2eWorkflow), "e2e-tests")
	require.Less(t,
		strings.Index(e2eTestsJob, "checkout chain"),
		strings.Index(e2eTestsJob, "Set up Go"),
	)
	require.Contains(t, e2eTestsJob, "cache-dependency-path: |\n            go.sum\n            interchaintest/go.sum")

	buildxInstaller, err := os.ReadFile(filepath.Join(repoRoot, "scripts/install-buildx.sh")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(buildxInstaller), "BUILDX_VERSION=0.36.1")
	require.Contains(t, string(buildxInstaller), "BUILDX_LINUX_AMD64_SHA256=48af8a397ebd60178778bf63611dbcebe5f5e7a9be90eb9147b24b9587455778")

	trivyInstaller, err := os.ReadFile(filepath.Join(repoRoot, "scripts/install-trivy.sh")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(trivyInstaller), "TRIVY_VERSION=0.74.0")
	require.Contains(t, string(trivyInstaller), "TRIVY_LINUX_AMD64_SHA256=2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a")

	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(dockerfile), "FROM golang:1.26.7-alpine3.24@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS go-builder")
	require.Contains(t, string(dockerfile), `test "$(/code/build/manifestd version)" = "${VERSION}"`)
	require.Equal(t, 2, strings.Count(string(dockerfile), "libcrypto3=3.5.8-r0"))
	require.Equal(t, 2, strings.Count(string(dockerfile), "libssl3=3.5.8-r0"))
	require.Contains(t, string(dockerfile), "jq=1.8.2-r0")
	require.Contains(t, string(dockerfile), "musl=1.2.6-r2")
	require.Contains(t, string(dockerfile), "musl-dev=1.2.6-r2")
	require.Contains(t, string(dockerfile), "linux-headers=7.0.0-r1")
	require.Contains(t, string(dockerfile), "musl=1.2.5-r12")
	require.Contains(t, string(dockerfile), "chmod 1777 /go/pkg/mod/cache/download/github.com/manifest-network/manifest-ledger/@v")
	require.NotContains(t, string(dockerfile), "chmod -R")
	require.NotContains(t, string(dockerfile), "chmod 1777 /go/pkg/mod\n")
	require.Contains(t, string(dockerfile), "&& go mod verify")

	goreleaser, err := os.ReadFile(filepath.Join(repoRoot, ".goreleaser.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(goreleaser), "- CC=gcc")
	require.NotContains(t, string(goreleaser), "musl-gcc")

	dockerignore, err := os.ReadFile(filepath.Join(repoRoot, ".dockerignore")) //nolint:gosec
	require.NoError(t, err)
	dockerignoreLines := strings.Split(string(dockerignore), "\n")
	for _, requiredScript := range []string{
		"!scripts/",
		"!scripts/build-ldflags.sh",
		"!scripts/install-wasmvm-muslc.sh",
		"!scripts/validate-build-inputs.sh",
	} {
		require.Contains(t, dockerignoreLines, requiredScript)
	}
	require.NotContains(t, dockerignoreLines, "!scripts/**")
}

func TestCodeQLWorkflowUsesImmutableLocalCosmosQueries(t *testing.T) {
	const queryCommit = "95e3707788fe2b95c84b7bc8e5694977fdc95611"

	repoRoot := filepath.Clean("..")
	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/codeql.yml")) //nolint:gosec
	require.NoError(t, err)
	workflowText := string(workflow)

	require.Contains(t, workflowText, "permissions:\n      actions: read\n      contents: read\n      security-events: write")
	require.Equal(t, 2, strings.Count(workflowText,
		"uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"))
	require.Contains(t, workflowText, "repository: crypto-com/cosmos-sdk-codeql")
	require.Contains(t, workflowText, "ref: "+queryCommit)
	require.Contains(t, workflowText, "path: .codeql/cosmos-sdk-codeql")
	require.Equal(t, 2, strings.Count(workflowText, "persist-credentials: false"))
	require.Contains(t, workflowText,
		`test "$(git -C .codeql/cosmos-sdk-codeql rev-parse HEAD)" = "`+queryCommit+`"`)
	require.Contains(t, workflowText, "queries: ./.codeql/cosmos-sdk-codeql,security-and-quality")
	require.NotContains(t, workflowText, "queries: crypto-com/cosmos-sdk-codeql@")

	for _, path := range []string{"codeql-pack.lock.yml", "qlpack.yml", "src"} {
		require.Contains(t, workflowText, "            "+path+"\n")
	}
	require.Contains(t, workflowText, "sparse-checkout-cone-mode: false")
}

func TestSPDXReleaseProfileValidation(t *testing.T) {
	valid := `{
  "spdxVersion":"SPDX-2.3",
  "dataLicense":"CC0-1.0",
  "SPDXID":"SPDXRef-DOCUMENT",
  "name":"manifest-ledger",
  "documentNamespace":"https://example.com/spdx/manifest-ledger",
  "creationInfo":{"created":"2026-09-01T00:00:00Z","creators":["Tool: syft-1.51.1"]},
  "packages":[{"SPDXID":"SPDXRef-Package-manifestd","name":"manifestd"}],
  "relationships":[{"spdxElementId":"SPDXRef-DOCUMENT","relationshipType":"DESCRIBES","relatedSpdxElement":"SPDXRef-Package-manifestd"}]
}`

	tests := []struct {
		name      string
		document  string
		wantError bool
	}{
		{name: "complete Syft profile", document: valid},
		{name: "unsupported SPDX version", document: strings.Replace(valid, "SPDX-2.3", "SPDX-2.2", 1), wantError: true},
		{name: "no packages", document: strings.Replace(valid, `"packages":[{"SPDXID":"SPDXRef-Package-manifestd","name":"manifestd"}]`, `"packages":[]`, 1), wantError: true},
		{name: "no document relationship", document: strings.Replace(valid, `"relationshipType":"DESCRIBES"`, `"relationshipType":"CONTAINS"`, 1), wantError: true},
		{name: "unrelated describes relationship", document: strings.Replace(valid, `"spdxElementId":"SPDXRef-DOCUMENT"`, `"spdxElementId":"SPDXRef-Other"`, 1), wantError: true},
		{name: "described package is absent", document: strings.Replace(valid, `"relatedSpdxElement":"SPDXRef-Package-manifestd"`, `"relatedSpdxElement":"SPDXRef-Package-missing"`, 1), wantError: true},
		{name: "malformed JSON", document: `{`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			sbom := filepath.Join(testDir, "release.sbom.json")
			require.NoError(t, os.WriteFile(sbom, []byte(tt.document), 0o600))
			cmd := exec.Command("sh", "./verify-spdx-sbom.sh", sbom) //nolint:gosec
			cmd.Dir = "."
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
			_, err := cmd.CombinedOutput()
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestReleaseTagTargetVerificationFailsClosed(t *testing.T) {
	const expected = "0123456789abcdef0123456789abcdef01234567"
	const firstTag = "1111111111111111111111111111111111111111"
	const secondTag = "2222222222222222222222222222222222222222"
	tests := []struct {
		name            string
		refObject       string
		firstTagObject  string
		secondTagObject string
		failAPI         bool
		wantError       bool
	}{
		{name: "matching lightweight tag", refObject: "commit:" + expected},
		{name: "matching annotated tag", refObject: "tag:" + firstTag, firstTagObject: "commit:" + expected},
		{name: "matching nested annotated tag", refObject: "tag:" + firstTag, firstTagObject: "tag:" + secondTag, secondTagObject: "commit:" + expected},
		{name: "tag moved", refObject: "commit:abcdef0123456789abcdef0123456789abcdef01", wantError: true},
		{name: "malformed response", refObject: "commit:not-an-object-id", wantError: true},
		{name: "unsupported final object", refObject: "tree:" + expected, wantError: true},
		{name: "missing annotated tag object", refObject: "tag:" + firstTag, wantError: true},
		{name: "API failure", failAPI: true, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			fakeGH := filepath.Join(testDir, "gh")
			const fakeGHSource = `#!/bin/sh
set -eu
[ "${1-}" = api ] || exit 90
[ "${FAKE_GH_FAIL-}" != yes ] || exit 91
[ "${3-}" = --jq ] || exit 93
[ "${4-}" = '.object.type + ":" + .object.sha' ] || exit 94
case "${2-}" in
	repos/manifest-network/manifest-ledger/git/ref/tags/v2.4.0)
		printf '%s\n' "${FAKE_REF_OBJECT-}"
		;;
	repos/manifest-network/manifest-ledger/git/tags/1111111111111111111111111111111111111111)
		printf '%s\n' "${FAKE_FIRST_TAG_OBJECT-}"
		;;
	repos/manifest-network/manifest-ledger/git/tags/2222222222222222222222222222222222222222)
		printf '%s\n' "${FAKE_SECOND_TAG_OBJECT-}"
		;;
	*) exit 92 ;;
esac
`
			// The fake must be executable because the script resolves gh through PATH.
			require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHSource), 0o700)) //nolint:gosec
			failAPI := ""
			if tt.failAPI {
				failAPI = "yes"
			}

			cmd := exec.Command("sh", "./verify-release-tag-target.sh") //nolint:gosec
			cmd.Dir = "."
			cmd.Env = []string{
				"PATH=" + testDir + ":" + os.Getenv("PATH"),
				"LC_ALL=C",
				"GITHUB_REPOSITORY=manifest-network/manifest-ledger",
				"GITHUB_REF_NAME=v2.4.0",
				"GITHUB_SHA=" + expected,
				"FAKE_REF_OBJECT=" + tt.refObject,
				"FAKE_FIRST_TAG_OBJECT=" + tt.firstTagObject,
				"FAKE_SECOND_TAG_OBJECT=" + tt.secondTagObject,
				"FAKE_GH_FAIL=" + failAPI,
			}
			_, err := cmd.CombinedOutput()
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func runReleaseCollisionCheck(t *testing.T, releaseTags, packageTags, failAPI string) error {
	t.Helper()
	testDir := t.TempDir()
	fakeGH := filepath.Join(testDir, "gh")
	const fakeGHSource = `#!/bin/sh
set -eu
[ "${1-}" = api ] || exit 90
for argument do
	case "$argument" in
		repos/*/releases*)
			[ "${FAKE_GH_FAIL-}" != releases ] || exit 91
			printf '%s\n' "${FAKE_RELEASE_TAGS-}"
			exit 0
			;;
		orgs/*/packages/*)
			[ "${FAKE_GH_FAIL-}" != packages ] || exit 92
			printf '%s\n' "${FAKE_PACKAGE_TAGS-}"
			exit 0
			;;
	esac
done
exit 93
`
	// The fake must be executable because the script resolves gh through PATH.
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHSource), 0o700)) //nolint:gosec

	cmd := exec.Command("sh", "./check-release-collisions.sh") //nolint:gosec
	cmd.Dir = "."
	cmd.Env = []string{
		"PATH=" + testDir + ":" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GITHUB_REPOSITORY=manifest-network/manifest-ledger",
		"GITHUB_REPOSITORY_OWNER=manifest-network",
		"GITHUB_REF_NAME=v2.4.0",
		"IMAGE_NAME=manifest-network/manifest-ledger",
		"FAKE_RELEASE_TAGS=" + releaseTags,
		"FAKE_PACKAGE_TAGS=" + packageTags,
		"FAKE_GH_FAIL=" + failAPI,
	}
	_, err := cmd.CombinedOutput()
	return err
}
