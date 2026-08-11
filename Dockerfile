# Build
FROM golang:1.26-alpine AS build

ARG VERSION=dev

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/wroclaw-sky ./cmd/wroclaw-sky

# Run
FROM alpine:3.24

RUN apk add --no-cache ca-certificates wget

COPY --from=build /out/wroclaw-sky /usr/local/bin/wroclaw-sky

ENV PORT=8081
ENV LOG_FORMAT=json
ENV LOG_LEVEL=info
ENV TRAILS_FILE=/data/trails.json
EXPOSE 8081

RUN mkdir -p /data && chown nobody:nobody /data
VOLUME ["/data"]

USER nobody

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:${PORT}/healthz || exit 1

ENTRYPOINT ["wroclaw-sky"]
