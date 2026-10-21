# syntax=docker/dockerfile:1.6

# Build stage runs natively on the build host and cross-compiles to the target
# platform. glebarez/sqlite is pure Go (no CGO), so this is a plain cross-compile.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS builder
# nodejs/npm: go:generate bundles web/src with esbuild, which resolves
# react/react-dom imports from web/node_modules — `npm ci` below installs them.
RUN apk add --no-cache esbuild make curl openssl bash nodejs npm
WORKDIR /src
COPY . .
RUN npm ci --prefix web
# `make generate` (not `go generate` directly) — it also runs
# vendor-frontend-js, which downloads and checksum-verifies
# React/react-dom/marked/DOMPurify into internal/frontend/static/js/vendor/
# before go:embed reads the tree. Calling `go generate` here bypassed that
# step entirely: the build was green and the binary embedded an empty
# vendor directory, so the shipped image 404'd on every vendored asset
# with no error anywhere in the pipeline.
RUN make generate
RUN for f in internal/frontend/static/js/vendor/*.js internal/frontend/static/fonts/*.woff2; do \
      [ -s "$f" ] || { echo "vendored asset missing or empty: $f" >&2; exit 1; }; \
    done

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/rendezvous ./cmd/rendezvous

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S rendezvous && adduser -S rendezvous -G rendezvous
WORKDIR /app
COPY --from=builder --chown=rendezvous:rendezvous /out/rendezvous .
# Writable directory for the default sqlite DSN (data/rendezvous.db).
RUN mkdir -p /app/data && chown -R rendezvous:rendezvous /app
USER rendezvous
EXPOSE 8080
ENTRYPOINT ["./rendezvous"]
