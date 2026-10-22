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

# No shell, no package manager: the binary and CA certificates only.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION
LABEL org.opencontainers.image.title="Glowpod" \
      org.opencontainers.image.description="Kubernetes dashboard: people sign in and act as themselves" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=api /glowpod /glowpod
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/glowpod", "-mode=server"]
