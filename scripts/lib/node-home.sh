#!/usr/bin/env bash
# Shared path checks for local-node/bootstrap helpers. Source from Bash.

resolve_node_home() {
  local node_home_input=${1:-"$HOME/.manifest"}
  if ! command -v realpath >/dev/null 2>&1; then
    printf '%s\n' 'realpath with -m support is required to resolve HOME_DIR safely.' >&2
    return 1
  fi
  case "$node_home_input" in
    \~) node_home_input=$HOME ;;
    \~/*) node_home_input="$HOME/${node_home_input#\~/}" ;;
    \~*) printf '%s\n' 'HOME_DIR supports only ~ or ~/ tilde expansion.' >&2; return 1 ;;
  esac
  if [[ "$node_home_input" == *$'\n'* || "$node_home_input" == *$'\r'* ]]; then
    printf '%s\n' 'HOME_DIR must not contain line breaks.' >&2
    return 1
  fi
  realpath -m -- "$node_home_input"
}

validate_node_home() {
  local node_home_target=$1 node_home_protected node_home_repo
  node_home_repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
  # A node home must be a dedicated directory, never a system root or an
  # ancestor of the user's home, checkout, working directory, or temp root.
  if [[ "$node_home_target" == / || "${node_home_target%/*}" == '' ]]; then
    printf 'Unsafe HOME_DIR: %s\n' "$node_home_target" >&2
    return 1
  fi
  for node_home_protected in "$HOME" "$node_home_repo" "$PWD" "${TMPDIR:-/tmp}" /tmp /var/tmp; do
    node_home_protected=$(realpath -m -- "$node_home_protected")
    case "$node_home_protected/" in
      "$node_home_target/"*) printf 'Unsafe HOME_DIR: %s\n' "$node_home_target" >&2; return 1 ;;
    esac
  done
  if [[ -e "$node_home_target" && ! -d "$node_home_target" ]]; then
    printf 'HOME_DIR is not a directory: %s\n' "$node_home_target" >&2
    return 1
  fi
}

require_explicit_reset_home() {
  if [[ -z ${1:-} ]]; then
    printf '%s\n' 'Set HOME_DIR explicitly to a disposable node directory before resetting it.' >&2
    return 1
  fi
}

update_node_genesis() {
  local node_genesis="$HOME_DIR/config/genesis.json" node_genesis_tmp
  node_genesis_tmp=$(mktemp "$HOME_DIR/config/genesis.XXXXXX")
  if ! jq "$@" "$node_genesis" >"$node_genesis_tmp"; then
    rm -f -- "$node_genesis_tmp"
    return 1
  fi
  mv -- "$node_genesis_tmp" "$node_genesis"
}
