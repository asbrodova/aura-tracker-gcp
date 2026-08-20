# ── Builder ───────────────────────────────────────────────────────────────────
FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

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
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates graphviz

COPY --from=builder /out/aura-tracker-gcp /aura-tracker-gcp

# These must be overridden at runtime with -e flags.
ENV GCP_PROJECT_ID=""
ENV GCP_ENVIRONMENTS_JSON=""
ENV GOOGLE_APPLICATION_CREDENTIALS=""
ENV GRAPHVIZ_DOT_PATH="/usr/bin/dot"

USER 65532:65532

ENTRYPOINT ["/aura-tracker-gcp"]
