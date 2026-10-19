FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
ARG GOPROXY=https://proxy.golang.org,direct
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/assets.go ./web/assets.go
COPY --from=frontend /src/web/dist/ ./web/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /portpilot ./cmd/portpilot

FROM alpine:3.23
LABEL org.opencontainers.image.source="https://github.com/maodeyu180/PortPilot" \
      org.opencontainers.image.title="PortPilot" \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 portpilot \
    && mkdir -p /app/data && chown 10001:10001 /app/data && chmod 700 /app/data
COPY --from=backend /portpilot /usr/local/bin/portpilot
USER 10001:10001
WORKDIR /app
EXPOSE 8787
ENTRYPOINT ["portpilot"]
CMD ["-listen", "0.0.0.0:8787", "-data", "/app/data"]
