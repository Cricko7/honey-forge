#!/bin/sh
set -eu
if [ "${NM_STDIO:-}" = 1 ]; then
  [ "${1:-}" = --worker ] || { echo 'Isolated worker mode required' >&2; exit 1; }
  # TLS telemetry uses stdin/stdout. Only replies to inbound decoy connections
  # may leave this container; no connections to management or other traps.
  iptables -P OUTPUT DROP
  # Docker DNS lives on loopback; allowing lo would expose service discovery
  # and an upstream DNS channel. Pipes need no loopback network access.
  iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED --ctdir REPLY -j ACCEPT
  [ "$(cat /proc/sys/net/ipv6/conf/all/disable_ipv6)" = 1 ]
  exec su-exec 65532:65532 /usr/local/bin/node-metrics "$@"
fi
exec /usr/local/bin/node-metrics "$@"
