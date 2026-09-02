FROM golang:1.26.7-alpine3.24@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS go-builder
ARG BUILD_CMD=build
ARG COMMIT=unknown
ARG VERSION=dev

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

# Pin direct dependencies against the explicit Alpine release used by the base
# image. Alpine repositories do not retain every superseded revision, so a
# security rebuild intentionally fails closed until these pins are reviewed.
RUN apk add --no-cache \
    ca-certificates=20260611-r0 \
    build-base=0.5-r4 \
    git=2.54.0-r0 \
    libcrypto3=3.5.8-r0 \
    libssl3=3.5.8-r0 \
    linux-headers=7.0.0-r1 \
    musl=1.2.6-r2 \
    musl-dev=1.2.6-r2

WORKDIR /code

COPY go.mod go.sum ./
COPY scripts/install-wasmvm-muslc.sh ./scripts/
# GoReleaser's module metadata probe writes a stat-cache entry for the main
# module even with GOPROXY=off. Keep dependency cache entries immutable and
# permit writes only in this otherwise empty main-module @v namespace.
RUN ./scripts/install-wasmvm-muslc.sh \
  && go mod download \
  && go mod verify \
  && mkdir -p /go/pkg/mod/cache/download/github.com/manifest-network/manifest-ledger/@v \
  && chmod 1777 /go/pkg/mod/cache/download/github.com/manifest-network/manifest-ledger/@v

# Copy over code
COPY . /code

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
# then log output of file /code/bin/manifestd
# then ensure static linking
RUN sh ./scripts/validate-build-inputs.sh build-command "${BUILD_CMD}" \
  && VERSION="${VERSION}" COMMIT="${COMMIT}" LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make "${BUILD_CMD}" \
  && file /code/build/manifestd \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/manifestd | grep "statically linked") \
  && test "$(/code/build/manifestd version)" = "${VERSION}"

# --------------------------------------------------------
FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

SHELL ["/bin/sh", "-ec"]

COPY --from=go-builder /code/build/manifestd /usr/bin/manifestd

# Starship's bring-your-own-chain contract requires these tools:
# https://docs.hyperweb.io/starship/development/add-new-chain#required-dependencies
RUN apk add --no-cache \
      ca-certificates-bundle=20260611-r0 \
      bash=5.2.37-r0 \
      curl=8.14.1-r3 \
      jq=1.8.2-r0 \
      libcrypto3=3.5.8-r0 \
      libssl3=3.5.8-r0 \
      make=4.4.1-r3 \
      musl=1.2.5-r12 \
      sed=4.9-r2; \
    command -v bash curl jq make sed >/dev/null

WORKDIR /opt

# rest server, tendermint p2p, tendermint rpc
EXPOSE 1317 26656 26657

CMD ["/usr/bin/manifestd", "version"]
