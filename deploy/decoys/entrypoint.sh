#!/bin/sh
set -eu
if [ "${NM_STDIO:-}" = 1 ]; then
  [ "${1:-}" = --worker ] || { echo 'Isolated worker mode required' >&2; exit 1; }
  # TLS telemetry uses stdin/stdout. Only replies to inbound decoy connections
  # may leave this container; no connections to management or other traps.
  iptables -P OUTPUT DROP
  iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  iptables -A OUTPUT -o lo -j ACCEPT
  [ "$(cat /proc/sys/net/ipv6/conf/all/disable_ipv6)" = 1 ]
  exec su-exec 65532:65532 /usr/local/bin/node-metrics "$@"
fi
exec /usr/local/bin/node-metrics "$@"
