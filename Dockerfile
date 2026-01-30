# Stage 1: Build
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install ca-certificates for HTTPS requests
RUN apk add --no-cache ca-certificates

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o engram ./cmd/engram

# Stage 2: Runtime
FROM gcr.io/distroless/static-debian12

# Copy CA certificates for HTTPS (OpenAI API calls)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /build/engram /usr/local/bin/engram

# Create data directory mount point
VOLUME /data

# Default port
EXPOSE 8080

# Run as non-root user (distroless provides nonroot user)
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/engram"]
