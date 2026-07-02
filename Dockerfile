ARG VERSION=dev

#build stage
FROM golang:1.25.11-alpine AS builder
ARG VERSION
RUN apk add --no-cache git
WORKDIR /go/src/app
COPY . .
RUN go mod download
RUN go build -ldflags "-X github.com/gabrielmbarboza/dealer/config.Version=${VERSION}" -o /go/bin/app -v ./cmd/dealer

#final stage
FROM alpine:3.24.1
ARG VERSION
RUN apk --no-cache add ca-certificates
COPY --from=builder /go/bin/app /app
COPY --from=builder /go/src/app/config.yml /config.yml
ENTRYPOINT /app
LABEL Name=dealer Version=${VERSION}
EXPOSE 3000
