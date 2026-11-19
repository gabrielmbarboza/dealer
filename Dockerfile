ARG VERSION=dev

#build stage
# The build stage runs on the builder's own architecture and cross-builds for the target
# (amd64 and arm64 in CI). BuildKit sets BUILDPLATFORM; the default keeps the classic
# builder working.
ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS builder
ARG VERSION
ARG TARGETOS
ARG TARGETARCH
RUN apk add --no-cache git
WORKDIR /go/src/app
COPY go.work go.work.sum ./
COPY cmd/dealer/go.mod cmd/dealer/go.sum cmd/dealer/
COPY config/go.mod config/
COPY gateway/go.mod gateway/go.sum gateway/
COPY loadtest/go.mod loadtest/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X github.com/gabrielmbarboza/dealer/config.Version=${VERSION}" -o /go/bin/app -v ./cmd/dealer

#final stage
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG VERSION
RUN apk --no-cache upgrade && apk --no-cache add ca-certificates
RUN addgroup -S dealer && adduser -S -G dealer dealer
# Writable home for the ACME certificate cache. A named volume mounted here
# starts with this ownership, so the unprivileged user can write to it.
RUN mkdir -p /var/lib/dealer/acme && chown -R dealer:dealer /var/lib/dealer
ENV DEALER_ACME_CACHE_DIR=/var/lib/dealer/acme
COPY --from=builder /go/bin/app /app
COPY --from=builder /go/src/app/conf/config.yml /config.yml
USER dealer
ENTRYPOINT ["/app"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/app", "healthcheck"]
LABEL Name=dealer Version=${VERSION}
EXPOSE 3000
