FROM golang:1.26-alpine AS build
WORKDIR /src
COPY src/backend/go.mod src/backend/go.sum ./
RUN go mod download
COPY src/backend ./
RUN CGO_ENABLED=0 go build -p 1 -trimpath -o /out/agent ./cmd/agent

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/agent /agent
ENTRYPOINT ["/agent"]

FROM runtime AS tcp
ENV DECOY_TYPE=tcp-banner

FROM runtime AS redis
ENV DECOY_TYPE=redis-emulator

FROM runtime AS honeytokens
ENV DECOY_TYPE=honeytoken-http
