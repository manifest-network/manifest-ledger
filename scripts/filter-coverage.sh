#!/bin/sh
set -eu
if [ "$#" -ne 2 ]; then
  echo "usage: $0 <profile> <filtered-profile>" >&2
  exit 64
fi
exec python3 "$(dirname "$0")/coverage_policy.py" "$1" "$2"
