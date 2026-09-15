FROM golang:1.25-alpine AS go-builder
ARG BUILD_CMD=build
ARG BUILD_TAGS=muslc
ARG VERSION

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

RUN apk add --no-cache ca-certificates build-base git curl

WORKDIR /code

ADD go.mod go.sum ./
COPY scripts/download-wasmvm.sh scripts/wasmvm-checksums.txt ./scripts/
RUN set -eux; \
    WASMVM_VERSION=$(go list -m -f '{{.Version}}' github.com/CosmWasm/wasmvm/v2); \
    sh ./scripts/download-wasmvm.sh "$WASMVM_VERSION" "$(uname -m)" /lib; \
    go mod download;

# Copy over code
COPY . /code

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
# then log output of file /code/bin/manifestd
# then ensure static linking
# An omitted or empty override leaves version selection to the Makefile.
RUN if [ -z "${VERSION:-}" ]; then unset VERSION; fi; \
    LEDGER_ENABLED=false BUILD_TAGS="$BUILD_TAGS" LINK_STATICALLY=true make "$BUILD_CMD" \
  && file /code/build/manifestd \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/manifestd | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.22

COPY --from=go-builder /code/build/manifestd /usr/bin/manifestd

# Install dependencies used for Starship
RUN apk add --no-cache curl make bash jq sed

WORKDIR /opt

# rest server, tendermint p2p, tendermint rpc
EXPOSE 1317 26656 26657

CMD ["/usr/bin/manifestd", "version"]
