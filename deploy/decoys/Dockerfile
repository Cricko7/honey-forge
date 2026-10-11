FROM golang:1.26-alpine AS build
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
ENV NM_ROLE=tcp-banner

FROM runtime AS redis
ENV NM_ROLE=redis-emulator

FROM runtime AS honeytokens
ENV NM_ROLE=honeytoken-http

# Trusted SSH gateway. Attacker processes run only in the separate sandbox image.
FROM runtime AS ssh
ENV NM_ROLE=ssh-high-interaction
