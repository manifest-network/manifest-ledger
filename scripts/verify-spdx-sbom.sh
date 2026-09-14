#!/bin/sh

set -eu

if [ "$#" -ne 3 ]; then
	printf '%s\n' "usage: $0 <spdx-json> <archive> <manifestd>" >&2
	exit 64
fi

spdx_python=${SPDX_PYTHON:-python3}
if ! command -v "$spdx_python" >/dev/null 2>&1; then
	printf '%s\n' "required SPDX verifier interpreter is unavailable: $spdx_python" >&2
	exit 69
fi

exec "$spdx_python" -B "$(dirname "$0")/verify_spdx_sbom.py" "$1" "$2" "$3"
