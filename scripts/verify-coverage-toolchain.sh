#!/bin/sh
set -eu

# Coverage counter layout and statement counts can change between Go versions.
# The host tests and Docker-built daemon must therefore use the same compiler.
if [ "$#" -ne 1 ]; then
    printf '%s\n' 'usage: verify-coverage-toolchain.sh HOST_GOVERSION' >&2
    exit 1
fi

builder_version=$(sed -n 's/^FROM golang:\([0-9][0-9.]*\)-[^ ]* AS go-builder$/go\1/p' Dockerfile)
if [ -z "$builder_version" ]; then
    printf '%s\n' 'cannot identify the pinned Go builder version in Dockerfile' >&2
    exit 1
fi
if [ "$1" != "$builder_version" ]; then
    printf '%s\n' \
        "coverage requires matching host and container Go versions: host=$1, container=$builder_version" \
        "run GOTOOLCHAIN=$builder_version make local-image-cover coverage" >&2
    exit 1
fi
