# syntax=docker/dockerfile:1
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY src/backend/go.mod src/backend/go.sum ./
RUN go mod download
COPY src/backend ./
RUN CGO_ENABLED=0 go build -p 1 -trimpath -ldflags="-s -w" -o /out/node-metrics ./cmd/agent

RUN CGO_ENABLED=0 go test -tags=integration -c -o /out/dionaea-test ./internal/decoys/dionaeatrap

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

FROM redis AS http
RUN apk add --no-cache python3
ENV NM_ROLE=custom-http PYTHONDONTWRITEBYTECODE=1

# Trusted SSH gateway. Attacker processes run only in the separate sandbox image.
FROM runtime AS ssh
ENV NM_ROLE=ssh-high-interaction

# Pin upstream code and its dependency lock. No tools or source checkout are
# fetched when a trap starts. Keep the upstream licence alongside the engine.
FROM ghcr.io/astral-sh/uv:0.9.3 AS conpot-uv
FROM python:3.14-slim AS conpot-build
RUN apt-get update && apt-get install -y --no-install-recommends git gcc libc6-dev libffi-dev libssl-dev \
    && rm -rf /var/lib/apt/lists/*
COPY --from=conpot-uv /uv /usr/local/bin/uv
WORKDIR /opt/conpot
RUN git init . \
    && git remote add origin https://github.com/mushorg/conpot.git \
    && git fetch --depth=1 origin 839f892a94c5dc9ced67d5c2f6a4e618a658a050 \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = 839f892a94c5dc9ced67d5c2f6a4e618a658a050 \
    && uv sync --frozen --no-dev --no-editable --python /usr/local/bin/python3 \
    && mkdir -p /opt/conpot/.venv/lib/python3.14/site-packages/conpot/tests/data/data_temp_fs

FROM python:3.14-slim AS conpot
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates iptables gosu docker-cli busybox \
    && ln -s /usr/sbin/gosu /usr/local/bin/su-exec \
    && ln -s /usr/bin/busybox /usr/local/bin/nslookup \
    && ln -s /usr/bin/busybox /usr/local/bin/pidof \
    && rm -rf /var/lib/apt/lists/*
COPY --from=conpot-build /opt/conpot/.venv /opt/conpot/.venv
COPY --from=conpot-build /opt/conpot/LICENSE.txt /usr/share/doc/conpot/LICENSE.txt
COPY --from=build /out/node-metrics /usr/local/bin/node-metrics
COPY deploy/decoys/entrypoint.sh /entrypoint.sh
ENV NM_ROLE=conpot HF_PYTHON=/opt/conpot/.venv/bin/python PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]

# Native Dionaea has legacy Cython bindings; Debian's Python 3.11/Cython 0.29
# build them without unpinned pip dependencies. Only the Python module is built.
FROM debian:bookworm-slim AS dionaea-build
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git cmake gcc make pkg-config python3-dev python3-distutils \
    python3-setuptools cython3 libglib2.0-dev libev-dev libudns-dev libssl-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /source
RUN git init . \
    && git remote add origin https://github.com/DinoTools/dionaea.git \
    && git fetch --depth=1 origin 4e459f1b672a5b4c1e8335c0bff1b93738019215 \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = 4e459f1b672a5b4c1e8335c0bff1b93738019215
COPY deploy/decoys/dionaea-build.py /tmp/dionaea-build.py
COPY deploy/decoys/dionaea-build_test.py /tmp/dionaea-build_test.py
RUN python3 -B /tmp/dionaea-build_test.py
RUN python3 /tmp/dionaea-build.py \
    && cmake -S . -B /build -DCMAKE_BUILD_TYPE=Release -DRELEASE_BUILD=ON \
       -DCMAKE_INSTALL_PREFIX=/opt/dionaea -DCMAKE_INSTALL_LIBDIR=lib \
       -DPYTHON_EXECUTABLE=/usr/bin/python3 \
       -DWITH_MODULE_CURL=OFF -DWITH_MODULE_EMU=OFF -DWITH_MODULE_NFQ=OFF \
       -DWITH_MODULE_NL=OFF -DWITH_MODULE_PCAP=OFF \
    && cmake --build /build --parallel 2 \
    && cmake --install /build

FROM debian:bookworm-slim AS dionaea
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates iptables gosu docker.io busybox python3 python3-yaml \
    libpython3.11 libglib2.0-0 libev4 libudns0 libssl3 \
    && ln -s /usr/sbin/gosu /usr/local/bin/su-exec \
    && ln -s /usr/bin/busybox /usr/local/bin/nslookup \
    && ln -s /usr/bin/busybox /usr/local/bin/pidof \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/dionaea /opt/dionaea/share/http-root \
    && chown 65532:65532 /run/dionaea
COPY --from=dionaea-build /opt/dionaea /opt/dionaea
COPY deploy/decoys/dionaea.cfg /opt/dionaea/etc/honeyforge.cfg
COPY src/backend/internal/decoys/internal/enginepipe/channel.py /opt/dionaea/lib/dionaea/python/dionaea/local_channel.py
COPY src/backend/internal/decoys/dionaeatrap/scripts/connections.py /opt/dionaea/lib/dionaea/python/dionaea/local_sessions.py
COPY src/backend/internal/decoys/dionaeatrap/scripts/observer.py /opt/dionaea/lib/dionaea/python/dionaea/local_observer.py
RUN printf '<html><body><h1>It works!</h1></body></html>\n' > /opt/dionaea/share/http-root/index.html
# Fail the image build if a selected upstream protocol or event bridge breaks.
RUN --mount=type=bind,from=build,source=/out/dionaea-test,target=/tmp/dionaea-test \
    TEST_DIONAEA_RUNTIME=1 gosu 65532:65532 /tmp/dionaea-test -test.run TestRealDionaea -test.v
COPY --from=build /out/node-metrics /usr/local/bin/node-metrics
COPY deploy/decoys/entrypoint.sh /entrypoint.sh
ENV NM_ROLE=dionaea PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
