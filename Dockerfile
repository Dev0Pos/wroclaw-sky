# Build
FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wroclaw-sky ./cmd/wroclaw-sky

# Run
FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=build /out/wroclaw-sky /usr/local/bin/wroclaw-sky

ENV PORT=8081
ENV LOG_FORMAT=json
ENV LOG_LEVEL=info
EXPOSE 8081

USER nobody
ENTRYPOINT ["wroclaw-sky"]
