#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: $0 <local-production-image>" >&2
  exit 64
fi
image=$1
test "$(docker image inspect "$image" --format '{{.Config.User}}')" = '10001:10001'
test "$(docker image inspect "$image" --format '{{json .Config.Entrypoint}}')" = '["/usr/bin/manifestd"]'
probe_dir=$(mktemp -d)
volume=$(docker volume create)
config_container=""
cleanup() {
  if [ -n "$config_container" ]; then docker rm "$config_container" >/dev/null; fi
  docker volume rm "$volume" >/dev/null
  rm -rf "$probe_dir"
}
trap cleanup EXIT HUP INT TERM
docker run --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  --mount "type=volume,source=$volume,target=/home/manifest/.manifest" \
  "$image" init production-smoke --chain-id production-smoke --default-denom umfx >"$probe_dir/init.log" 2>&1
config_container=$(docker create --mount "type=volume,source=$volume,target=/home/manifest/.manifest" "$image" version)
docker cp "$config_container:/home/manifest/.manifest/config/app.toml" "$probe_dir/app.toml"
grep -Eq '^query-gas-limit = "5000000"$' "$probe_dir/app.toml"
if docker run --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  --mount "type=volume,source=$volume,target=/home/manifest/.manifest" \
  "$image" start --query-gas-limit 0 >"$probe_dir/zero-gas.log" 2>&1; then
  echo "production startup accepted an unbounded query budget" >&2
  exit 1
fi
grep -F 'must be a positive finite gas budget' "$probe_dir/zero-gas.log"
# A second unprivileged container must read and validate the persisted home.
docker run --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  --mount "type=volume,source=$volume,target=/home/manifest/.manifest" \
  "$image" genesis validate-genesis
docker run --rm --network none --read-only --tmpfs /tmp:rw,nosuid,nodev \
  --mount "type=volume,source=$volume,target=/home/manifest/.manifest" \
  "$image" version
