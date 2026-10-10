# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/0xTraceSentinel/MediaScraper/internal/version.Version=${VERSION}" \
      -o /out/mediascraper ./cmd/mediascraper

# Distroless static image: CA certificates included, no shell, non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mediascraper /usr/local/bin/mediascraper
ENTRYPOINT ["mediascraper"]
