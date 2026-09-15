#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: $0 <exported-production-rootfs>" >&2
  exit 64
fi
rootfs=$1
test -d "$rootfs" && test ! -L "$rootfs"
test -x "$rootfs/usr/bin/manifestd"
test -s "$rootfs/etc/ssl/certs/ca-certificates.crt"
# The scratch runtime has no package database, shell, or other regular files.
# Reject symlinks and special files rather than following them during checks.
test -z "$(find "$rootfs" ! -type d ! -type f -print -quit)"
files=$(find "$rootfs" -type f -printf '%P\n' | LC_ALL=C sort)
expected=$(printf '%s\n' etc/ssl/certs/ca-certificates.crt usr/bin/manifestd)
if [ "$files" != "$expected" ]; then
  echo "production runtime contains unexpected files" >&2
  exit 1
fi
