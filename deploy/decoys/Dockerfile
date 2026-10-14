FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY src/backend/go.mod src/backend/go.sum ./
RUN go mod download
COPY src/backend ./
RUN CGO_ENABLED=0 go build -p 1 -trimpath -ldflags="-s -w" -o /out/node-metrics ./cmd/agent

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates docker-cli iptables su-exec
COPY --from=build /out/node-metrics /usr/local/bin/node-metrics
COPY deploy/decoys/entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]

FROM runtime AS tcp
ENV NM_ROLE=tcp

FROM redis:7.4.6-alpine AS redis-binary

# Do not inherit the official image's /data VOLUME: workers have no disk mounts.
FROM alpine:3.21 AS redis
RUN apk add --no-cache ca-certificates docker-cli iptables su-exec libssl3 libcrypto3
COPY --from=redis-binary /usr/local/bin/redis-server /usr/local/bin/redis-server
COPY --from=build /out/node-metrics /usr/local/bin/node-metrics
COPY deploy/decoys/entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
ENV NM_ROLE=redis-emulator

FROM runtime AS honeytokens
ENV NM_ROLE=honeytoken-http

FROM runtime AS http
RUN apk add --no-cache python3
ENV NM_ROLE=custom-http PYTHONDONTWRITEBYTECODE=1

# Trusted SSH gateway. Attacker processes run only in the separate sandbox image.
FROM runtime AS ssh
ENV NM_ROLE=ssh-high-interaction
