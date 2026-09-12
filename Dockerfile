# Build the binary in a full Go image, then ship it on a distroless base so the
# published image contains the server and nothing else.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src

# Download dependencies in their own layer so that a source-only change does
# not invalidate the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/ravibagri5/crossplane-mcp-server/pkg/version.Version=${VERSION} \
      -X github.com/ravibagri5/crossplane-mcp-server/pkg/version.Commit=${COMMIT} \
      -X github.com/ravibagri5/crossplane-mcp-server/pkg/version.BuildDate=${BUILD_DATE}" \
    -o /out/crossplane-mcp-server ./cmd/crossplane-mcp-server

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="crossplane-mcp-server" \
      org.opencontainers.image.description="Model Context Protocol server for Crossplane control planes" \
      org.opencontainers.image.source="https://github.com/ravibagri5/crossplane-mcp-server" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /out/crossplane-mcp-server /usr/local/bin/crossplane-mcp-server

USER nonroot:nonroot

# Default to stdio. Pass --http-address to serve HTTP instead.
ENTRYPOINT ["/usr/local/bin/crossplane-mcp-server"]
