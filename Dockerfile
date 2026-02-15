# Build Stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the local source code
COPY picoclaw/go.mod picoclaw/go.sum ./
RUN go mod download

COPY picoclaw/ .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o picoclaw ./cmd/picoclaw

# Runtime Stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/picoclaw /usr/local/bin/picoclaw

# Set up directories
RUN mkdir -p /app/config /app/workspace

# Set default command
ENTRYPOINT ["picoclaw"]
CMD ["gateway"]
