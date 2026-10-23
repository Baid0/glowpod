# syntax=docker/dockerfile:1
# Glowpod in one small, non-root image, for amd64 and arm64.
#   docker build -t glowpod .
#   docker buildx build --platform linux/amd64,linux/arm64 -t REGISTRY/glowpod:TAG --push .
# The UI and Go stages run on the build machine's platform and cross-compile,
# so multi-arch builds don't emulate anything.

ARG VERSION=dev

FROM --platform=$BUILDPLATFORM node:26-alpine AS ui
WORKDIR /src/frontend
RUN npm install -g pnpm@12.3.4
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS api
ARG TARGETOS TARGETARCH VERSION
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=ui /src/frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /glowpod ./cmd/glowpod

# Glowpod Hub in one container, like Rancher's Docker install: K3s inside,
# and the Hub on it. It needs docker run --privileged. Build it with
#   docker build --target hub -t glowpod-hub .
FROM rancher/k3s:v1.36.5-k3s1 AS hub
ARG VERSION
LABEL org.opencontainers.image.title="Glowpod Hub" \
      org.opencontainers.image.description="Glowpod Hub with K3s inside: many Kubernetes clusters, one sign-in" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/glowpod-dev/glowpod"
COPY --from=api /glowpod /bin/glowpod
# K3s, the Hub's CA and its state; keep it on a volume.
VOLUME /var/lib/glowpod
EXPOSE 80 443
ENTRYPOINT ["/bin/glowpod", "hub", "run"]

# The default image. No shell, no package manager: the binary and CA
# certificates only.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION
LABEL org.opencontainers.image.title="Glowpod" \
      org.opencontainers.image.description="Kubernetes dashboard: people sign in and act as themselves" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/glowpod-dev/glowpod"
COPY --from=api /glowpod /glowpod
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/glowpod", "-mode=server"]
