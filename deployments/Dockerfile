# syntax=docker/dockerfile:1
#
# One Dockerfile, two named final stages (api, mockserver) sharing one
# builder stage — avoids duplicating the build steps across two files.
# Build context is the repo root (see docker-compose.yml's `context: ..`),
# since both binaries need go.mod/internal/ from there.

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/mockserver ./mockserver

FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

FROM alpine:3.20 AS mockserver
COPY --from=builder /out/mockserver /usr/local/bin/mockserver
EXPOSE 9000
ENTRYPOINT ["/usr/local/bin/mockserver"]
