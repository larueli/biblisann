# syntax=docker/dockerfile:1
# Base images are pinned by digest (Dependabot proposes updates). VERSION is the release version.

FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS web
RUN npm install --global pnpm@12.21.0
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
# The Vite build writes to ../internal/webui/dist.
RUN mkdir -p /src/internal/webui && pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/larueli/biblisann/internal/version.version=${VERSION}" \
    -o /out/biblisann ./cmd/biblisann \
 && mkdir -p /out/data/covers

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
ARG VERSION=dev
LABEL org.opencontainers.image.title="Biblisann" \
      org.opencontainers.image.description="Self-hosted physical book library manager" \
      org.opencontainers.image.source="https://github.com/larueli/biblisann" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/biblisann /biblisann
# Writable data directory (covers). Mount a volume here; everything else is read-only.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
ENV BIBLISANN_DATA_DIR=/data
# Non-root (65532 "nonroot"), no shell, no package manager. Run with a read-only root filesystem.
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/biblisann", "healthcheck"]
ENTRYPOINT ["/biblisann"]
CMD ["serve"]
