FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG AETHER_VERSION=0.1.2
ARG AETHER_REVISION
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X aether/internal/app.Version=${AETHER_VERSION} -X aether/internal/app.Revision=${AETHER_REVISION}" -o /aether ./cmd/aether

FROM alpine:3.23
ARG AETHER_VERSION=0.1.2
ARG AETHER_REVISION
LABEL org.opencontainers.image.version="${AETHER_VERSION}" org.opencontainers.image.revision="${AETHER_REVISION}"
RUN apk add --no-cache ca-certificates tzdata fuse3 ffmpeg \
    && mkdir -p /config /data /mnt /app/web
WORKDIR /app
COPY --from=backend /aether /app/aether
COPY --from=web /src/web/dist /app/web
COPY THIRD_PARTY_NOTICES.md /app/THIRD_PARTY_NOTICES.md
ENV AETHER_PORT=15151 \
    AETHER_WEB_DIR=/app/web \
    TZ=Asia/Shanghai
EXPOSE 15151
VOLUME ["/config", "/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD port="${AETHER_ADDR##*:}"; wget -q -O /dev/null "http://127.0.0.1:${port:-${AETHER_PORT:-15151}}/api/health" || exit 1
ENTRYPOINT ["/app/aether"]
CMD ["-config-dir", "/config", "-data-dir", "/data"]
