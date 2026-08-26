# Build
FROM golang:1.27-alpine AS build

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/wroclaw-sky ./cmd/wroclaw-sky

# Certs + empty /data for trails volume (scratch has no package manager / shell).
FROM alpine:3.24 AS certs
RUN apk add --no-cache ca-certificates \
  && mkdir -p /data \
  && chown 65534:65534 /data

# Run — static binary only (avoids Alpine openssl CVEs in the final image).
FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs --chown=65534:65534 /data /data
COPY --from=build /out/wroclaw-sky /usr/local/bin/wroclaw-sky

ENV PORT=8081
ENV LOG_FORMAT=json
ENV LOG_LEVEL=info
ENV TRAILS_FILE=/data/trails.json
EXPOSE 8081
VOLUME ["/data"]

USER 65534:65534

# Probe via the binary (scratch image has no wget/shell).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/wroclaw-sky", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/wroclaw-sky"]
