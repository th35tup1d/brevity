# Stage 1: Build the Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -o /app/bin/brevity ./cmd/api

# Stage 2: Minimal runtime image
FROM alpine:3.19

WORKDIR /root/

# Copy the compiled binary
COPY --from=builder /app/bin/brevity .

# CRITICAL: Copy the web assets folder into the runtime container
COPY --from=builder /app/web ./web

EXPOSE 8080

CMD ["./brevity"]
