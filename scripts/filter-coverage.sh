#!/bin/sh
set -eu
if [ "$#" -ne 2 ]; then
  echo "usage: $0 <profile> <filtered-profile>" >&2
  exit 64
fi
python3 - "$1" "$2" <<'PYTHON'
from fnmatch import fnmatchcase
from pathlib import Path
import sys
patterns = [line.strip() for line in Path('.coverageignore').read_text().splitlines()
            if line.strip() and not line.lstrip().startswith('#')]
lines = Path(sys.argv[1]).read_text().splitlines(keepends=True)
filtered = [line for line in lines if line.startswith('mode:') or not any(
    fnmatchcase(line.split(':', 1)[0], pattern) for pattern in patterns)]
Path(sys.argv[2]).write_text(''.join(filtered))
PYTHON
