#!/usr/bin/env bash
set -euo pipefail

# Exercise Make's compiler selection without compiling binaries or building images.
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT
export COVERAGE_BUILD_LOG="$test_dir/docker.log"
export PATH="$test_dir:$PATH"

cat > "$test_dir/selected-go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  'env GOVERSION') printf '%s\n' "$TEST_GO_VERSION" ;;
  'env GOROOT') printf '%s\n' /selected/go/root ;;
  'version') printf 'go version %s linux/amd64\n' "$TEST_GO_VERSION" ;;
  *) echo "Unexpected Go invocation: $*" >&2; exit 1 ;;
esac
EOF
cat > "$test_dir/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$COVERAGE_BUILD_LOG"
EOF
chmod +x "$test_dir/selected-go" "$test_dir/docker"

for version in go1.25.9 go1.27.1; do
  export TEST_GO_VERSION="$version"
  : > "$COVERAGE_BUILD_LOG"
  make --no-print-directory local-image-coverage local-image-testnet-upgrade GO="$test_dir/selected-go"
  test "$(grep -Fc -- "--build-arg GO_VERSION=${version#go} " "$COVERAGE_BUILD_LOG")" -eq 2
  test "$(grep -Fc -- '--build-arg BUILD_CMD=build-coverage' "$COVERAGE_BUILD_LOG")" -eq 2
  grep -Fq -- '--build-arg VERSION=eng879-test-upgrade -t manifest-testnet-upgrade:local' "$COVERAGE_BUILD_LOG"

  make --no-print-directory -n coverage GO="$test_dir/selected-go" > "$test_dir/coverage-plan"
  test "$(grep -Fc -- "$test_dir/selected-go test " "$test_dir/coverage-plan")" -eq 2
  grep -Fq -- "$test_dir/selected-go list ./..." "$test_dir/coverage-plan"
  for tool in 'covdata merge' 'covdata textfmt' 'cover -func=' 'cover -html='; do
    grep -Fq -- "$test_dir/selected-go tool $tool" "$test_dir/coverage-plan"
  done
done

# An invalid/custom compiler must fail before either Docker build is invoked.
for version in '' 'devel go1.28' 'go1.27.1-X:nodwarf5'; do
  export TEST_GO_VERSION="$version"
  for target in local-image-coverage local-image-testnet-upgrade; do
    : > "$COVERAGE_BUILD_LOG"
    if make --no-print-directory "$target" GO="$test_dir/selected-go" > "$test_dir/rejected" 2>&1; then
      echo "Unexpected coverage image success for Go version '$version'" >&2
      exit 1
    fi
    grep -Fq 'Coverage images require an official Go release' "$test_dir/rejected"
    test ! -s "$COVERAGE_BUILD_LOG"
  done
done

# The production image target remains independent of the coverage compiler check.
: > "$COVERAGE_BUILD_LOG"
make --no-print-directory local-image GO="$test_dir/selected-go"
grep -Fxq 'build . -t manifest:local' "$COVERAGE_BUILD_LOG"

# Run only the real Make recipe's initialization against a disposable coverage root.
# Existing top-level metadata is retained but textfmt must read only the cleared merge dir.
export TEST_GO_VERSION=go1.25.9
coverage_root="$test_dir/coverage"
mkdir -p "$coverage_root"/{unit-e2e,simulation,merged}
touch "$coverage_root"/{unit-e2e,simulation,merged}/covmeta.stale "$coverage_root/covmeta.legacy"
make --no-print-directory -n coverage GO="$test_dir/selected-go" COV_ROOT="$coverage_root" > "$test_dir/coverage-plan"
sed '/Building instrumented simulation test binary/,$d' "$test_dir/coverage-plan" | bash
for directory in unit-e2e simulation merged; do
  test ! -e "$coverage_root/$directory/covmeta.stale"
done
test -f "$coverage_root/covmeta.legacy"
grep -Fq -- "tool covdata merge -i=\"$coverage_root/unit-e2e\",\"$coverage_root/simulation\" -o \"$coverage_root/merged\"" "$test_dir/coverage-plan"
grep -Fq -- "tool covdata textfmt -i=\"$coverage_root/merged\" -o $coverage_root/coverage-merged.out" "$test_dir/coverage-plan"

echo 'Coverage build selection and stale-output checks passed.'
