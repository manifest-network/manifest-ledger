#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

[ "$#" -ge 2 ] || fail "usage: $0 <checksum-manifest> <artifact>..."
command -v awk >/dev/null 2>&1 || fail "required checksum verifier command is unavailable: awk"

manifest=$1
shift

[ -f "$manifest" ] && [ ! -L "$manifest" ] || fail "checksum manifest must be a regular file: $manifest"

expected_names=
for artifact do
	[ -f "$artifact" ] && [ ! -L "$artifact" ] || fail "checksummed artifact must be a regular file: $artifact"
	name=${artifact##*/}
	case "$name" in
		"" | */*) fail "checksummed artifact must have a simple basename: $artifact" ;;
	esac
	if [ -z "$expected_names" ]; then
		expected_names=$name
	else
		expected_names="$expected_names
$name"
	fi
done

awk -v expected="$expected_names" '
	BEGIN {
		expected_count = split(expected, expected_names, "\n")
		for (position = 1; position <= expected_count; position++) {
			name = expected_names[position]
			if (name in allowed) {
				printf "duplicate expected artifact basename: %s\n", name > "/dev/stderr"
				invalid = 1
			}
			allowed[name] = 1
		}
	}
	{
		line = $0
		sub(/\r$/, "", line)
		hash = substr(line, 1, 64)
		separator = substr(line, 65, 2)
		filename = substr(line, 67)
		if (length(hash) != 64 || hash ~ /[^0-9a-fA-F]/ || (separator != "  " && separator != " *") || filename == "") {
			printf "malformed checksum entry at line %d\n", NR > "/dev/stderr"
			invalid = 1
			next
		}
		if (index(filename, "/") != 0 || !(filename in allowed)) {
			printf "unexpected checksum artifact at line %d: %s\n", NR, filename > "/dev/stderr"
			invalid = 1
			next
		}
		seen[filename]++
	}
	END {
		for (position = 1; position <= expected_count; position++) {
			name = expected_names[position]
			if (seen[name] != 1) {
				printf "checksum manifest must contain exactly one entry for %s (found %d)\n", name, seen[name] + 0 > "/dev/stderr"
				invalid = 1
			}
		}
		exit invalid
	}
' "$manifest" || fail "checksum manifest does not bind the exact release artifact set"
