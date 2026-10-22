# syntax=docker/dockerfile:1
# Glowpod in one small, non-root image. Build: docker build -t glowpod .

FROM node:26-alpine AS ui
WORKDIR /src/frontend
RUN npm install -g pnpm@12.3.4
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:1.27-alpine AS api
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=ui /src/frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /glowpod ./cmd/glowpod

# No shell, no package manager: just the binary and CA certificates.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=api /glowpod /glowpod
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/glowpod", "-mode=server"]
