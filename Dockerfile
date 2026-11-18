# ── Builder ───────────────────────────────────────────────────────────────────
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

WORKDIR /src

# Cache module downloads as a separate layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aura-tracker-gcp \
    ./cmd/aura-tracker-gcp

# ── Runtime ───────────────────────────────────────────────────────────────────
# Graphviz powers the optional SVG renderer. The process still runs as the
# distroless-compatible non-root uid/gid 65532.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

RUN apk add --no-cache ca-certificates graphviz

COPY --from=builder /out/aura-tracker-gcp /aura-tracker-gcp

# These must be overridden at runtime with -e flags.
ENV GCP_PROJECT_ID=""
ENV GCP_ENVIRONMENTS_JSON=""
ENV GOOGLE_APPLICATION_CREDENTIALS=""
ENV GRAPHVIZ_DOT_PATH="/usr/bin/dot"

USER 65532:65532

ENTRYPOINT ["/aura-tracker-gcp"]
