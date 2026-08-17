FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /ghost ./cmd/ghost
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /ghost-web ./cmd/ghost-web

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

RUN addgroup -S ghost && adduser -S -G ghost ghost

RUN mkdir -p /var/ghost/data /var/ghost/workspace /var/lib/ghost/workspace /etc/ghost

COPY --from=builder /ghost /usr/local/bin/ghost
COPY --from=builder /ghost-web /usr/local/bin/ghost-web

COPY config/config.example.json /etc/ghost/config.json

RUN chown -R ghost:ghost /var/ghost /var/lib/ghost /etc/ghost

USER ghost

EXPOSE 8766 80

VOLUME ["/var/ghost/data", "/var/ghost/workspace", "/var/lib/ghost/workspace"]

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:8766/v1/status || exit 1

ENTRYPOINT ["ghost"]
CMD ["agent"]
