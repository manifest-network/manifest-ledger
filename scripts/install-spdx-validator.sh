#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	printf '%s\n' "usage: $0 <new-venv-directory>" >&2
	exit 64
fi
venv_dir=$1
if [ -e "$venv_dir" ] || [ -L "$venv_dir" ]; then
	printf '%s\n' "SPDX validator installation requires a new directory: $venv_dir" >&2
	exit 1
fi

python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else "SPDX validator installation requires Python 3.11 or newer")'
python3 -m venv "$venv_dir"
"$venv_dir/bin/python" -m pip install --disable-pip-version-check \
	--require-hashes --only-binary=:all: \
	-r "$(dirname "$0")/spdx-validator/requirements.txt"
"$venv_dir/bin/python" -c 'from importlib.metadata import version; print("installed spdx-tools " + version("spdx-tools"))'
