# syntax=docker/dockerfile:1.7
# Base image digests resolved anonymously on 2026-09-30 via the Docker Hub
# and gcr.io registry APIs (see task-3-report.md for the exact commands).
FROM node:24-bookworm-slim@sha256:0e0ff40c39bc087845bfb27465a0df4ea419520094bc35842ff83dd8cbe6f9b6 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS build
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/build ./web/build
# Bit-exact terrain on the target microarchitecture level, or the build fails.
RUN go test ./internal/worldgen -run TestTerrainDigest -count=1
# Kept as a separate RUN (not merged with the test step above) so a test
# failure and a build failure show up as distinct, individually cached
# layers/log sections in CI.
# hadolint ignore=DL3059
RUN go build -tags webui -trimpath -ldflags "-s -w" -o /out/farsight ./cmd/farsight

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/farsight /usr/local/bin/farsight
USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/farsight"]
CMD ["serve", "-config", "/etc/farsight/farsight.json"]
