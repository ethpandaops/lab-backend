# Multi-stage build for lab-backend
FROM golang:1.25.1-alpine AS builder

RUN apk add --no-cache make git ca-certificates

WORKDIR /build

COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG GIT_COMMIT=dev
ARG BUILD_DATE=unknown

# Build the binary using make
RUN VERSION=${VERSION} GIT_COMMIT=${GIT_COMMIT} BUILD_DATE=${BUILD_DATE} make build

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/bin/lab-backend .

RUN chown -R appuser:appuser /app

USER appuser

# Expose HTTP port
EXPOSE 8080

# Run the binary
# Configuration should be provided via Kubernetes ConfigMap mounted as volume
ENTRYPOINT ["/app/lab-backend"]
