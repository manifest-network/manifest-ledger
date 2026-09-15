#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <manifestd-binary>" >&2
	exit 64
fi

binary="$1"
file_description="$(file "$binary")"
case "$file_description" in
	*"statically linked"*) ;;
	*)
		echo "release binary is not statically linked: $file_description" >&2
		exit 1
		;;
esac

"$binary" version >/dev/null
