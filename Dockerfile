FROM golang:1.22-alpine AS go-builder
ARG BUILD_CMD=build

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

RUN apk add --no-cache ca-certificates build-base git

WORKDIR /code

ADD go.mod go.sum ./
RUN set -eux; \
    export ARCH=$(uname -m); \
    if [ "$ARCH" = "x86_64" ]; then ARCH=amd64; fi; \
    WASM_VERSION=$(go list -m all | grep github.com/CosmWasm/wasmvm | awk '{print $2}'); \
    if [ ! -z "${WASM_VERSION}" ]; then \
      WASMVM_REPO=$(echo $WASM_VERSION | awk '{print $1}'); \
      WASMVM_VERS=$(echo $WASM_VERSION | awk '{print $2}'); \
      wget -O /lib/libwasmvm_muslc.a https://${WASMVM_REPO}/releases/download/${WASMVM_VERS}/libwasmvm_muslc.${ARCH}.a; \
      ln /lib/libwasmvm_muslc.a /lib/libwasmvm_muslc.${ARCH}.a; \
    fi; \
    go mod download;

# Copy over code
COPY . /code

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
# then log output of file /code/bin/manifestd
# then ensure static linking
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make $BUILD_CMD \
  && file /code/build/manifestd \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/manifestd | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.20

COPY --from=go-builder /code/build/manifestd /usr/bin/manifestd

# Install dependencies used for Starship
RUN apk add --no-cache curl make bash jq sed

WORKDIR /opt

# rest server, tendermint p2p, tendermint rpc
EXPOSE 1317 26656 26657

CMD ["/usr/bin/manifestd", "version"]
