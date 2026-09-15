#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: $0 <pinned-proto-builder-image>" >&2
  exit 64
fi
repo_root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"
read -r baseline_tag baseline_commit < .github/protobuf-baseline.ref
if [ "$(git rev-parse "${baseline_tag}^{commit}")" != "$baseline_commit" ]; then
  echo "released protobuf baseline tag does not match its pinned commit" >&2
  exit 1
fi
mkdir -p .review-tmp
baseline_dir=$(mktemp -d "$repo_root/.review-tmp/proto-baseline.XXXXXXXX")
trap 'rm -rf "$baseline_dir"' EXIT HUP INT TERM
git archive --format=tar "$baseline_commit" proto > "$baseline_dir/baseline.tar"
tar -xf "$baseline_dir/baseline.tar" -C "$baseline_dir"
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp \
  -v "$repo_root:/workspace:ro" --workdir /workspace "$1" \
  buf breaking proto/ --against ".review-tmp/$(basename "$baseline_dir")/proto/" --error-format=json
