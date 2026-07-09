# Build stage
FROM golang:1.25 AS builder

WORKDIR /build

# Copy go mod files first for better layer caching during rebuilds
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with size optimizations (-w removes DWARF debug info, -s removes symbol table)
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -ldflags="-w -s" --buildvcs=false -o subscriptions .

# Runtime stage - small debian-based image
FROM debian:stable-slim

# Install ca-certificates for HTTPS connections, then clean up apt cache
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN useradd -r -u 1000 -m -s /bin/false appuser

# Copy only the binary from builder
COPY --from=builder /build/subscriptions /bin/subscriptions

# Switch to non-root user
USER appuser

ENTRYPOINT ["subscriptions"]

EXPOSE 60000
