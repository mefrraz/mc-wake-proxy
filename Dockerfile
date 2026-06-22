# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
ARG TARGETARCH
ARG TARGETOS
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /mc-wake-proxy ./cmd/proxy/

# ---- Runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /mc-wake-proxy .
EXPOSE 25565 8080
ENTRYPOINT ["./mc-wake-proxy"]
