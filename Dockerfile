# syntax=docker/dockerfile:1

# --- Frontend: runs on the build host, output is architecture-independent.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

# --- Backend: cross-compiled on the build host, no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist/app ./web/dist/app
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
      -o /out/inventory ./cmd/inventory \
 && mkdir -p /out/data

# --- Runtime: distroless static (CA certificates, tzdata, no shell), non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/inventory /inventory
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV INVENTORY_DATA=/data INVENTORY_ADDR=:8080
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
# No HEALTHCHECK: the image has no shell or curl and the binary has no probe
# subcommand yet. See the healthcheck in docker-compose.yml.
ENTRYPOINT ["/inventory"]
