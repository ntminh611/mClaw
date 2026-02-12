# ── Build stage ──────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mclaw ./cmd/mclaw

# ── Runtime stage ────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /mclaw /usr/local/bin/mclaw

WORKDIR /app
VOLUME /app/mclawdata

ENTRYPOINT ["mclaw"]
CMD ["start"]
