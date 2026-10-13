#!/bin/sh
set -eu

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

if [ "$#" -eq 0 ]; then
  workers=$(docker ps --filter label=com.nodemetrics.worker --format '{{.ID}}')
  sandboxes=$(docker ps --filter label=com.nodemetrics.sandbox=true --format '{{.ID}}')
  set -- $workers $sandboxes
fi

[ "$#" -gt 0 ] || fail 'Start a managed TCP/Redis/HTTP trap or open an SSH session first.'

for container do
  worker=$(docker inspect --format '{{index .Config.Labels "com.nodemetrics.worker"}}' "$container")
  sandbox=$(docker inspect --format '{{index .Config.Labels "com.nodemetrics.sandbox"}}' "$container")
  [ -n "$worker" ] || [ "$sandbox" = true ] || fail 'Container is not a managed worker or sandbox.'

  policy=$(docker inspect --format '{{.HostConfig.Privileged}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.IpcMode}} {{.HostConfig.CgroupnsMode}}' "$container")
  [ "$policy" = 'false true private private' ] || fail 'Private namespaces or read-only root filesystem are missing.'

  pid=$(docker inspect --format '{{.HostConfig.PidMode}}' "$container")
  [ -z "$pid" ] || fail 'Container shares another PID namespace.'

  mounts=$(docker inspect --format '{{range .Mounts}}{{if ne .Type "tmpfs"}}{{println .Destination}}{{end}}{{end}}' "$container")
  [ -z "$mounts" ] || fail 'Container has a host or persistent volume mount.'

  core=$(docker inspect --format '{{range .HostConfig.Ulimits}}{{if eq .Name "core"}}{{.Soft}} {{.Hard}}{{end}}{{end}}' "$container")
  [ "$core" = '0 0' ] || fail 'Core dumps are enabled.'

  dns=$(docker inspect --format '{{range .HostConfig.Dns}}{{.}}{{end}} {{range .HostConfig.DnsSearch}}{{.}}{{end}}' "$container")
  [ "$dns" = '127.0.0.1 .' ] || fail 'Container inherited the host DNS configuration.'

  keys=$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container" | cut -d= -f1)
  for key in $keys; do
    case "$key" in
      NM_ROLE|NM_STDIO|SANDBOX_PUBLIC_KEY) ;;
      NM_*|AGENT_*|TRAP_*|DOCKER_*|POSTGRES_*|*TOKEN*|*PASSWORD*|*SECRET*) fail 'Container inherited management settings.' ;;
    esac
  done

  if [ "$sandbox" = true ]; then
    network=$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$container")
    [ "$network" = none ] || fail 'SSH sandbox has a network attachment.'

    docker exec --user 1000:1000 "$container" /bin/sh -ec '
      test ! -e /var/run/docker.sock && test ! -d /run/secrets
      test -z "$(tail -n +2 /proc/net/route)"
      if grep -El "^(node-metrics|orchestrator|redis-server)$" /proc/[0-9]*/comm 2>/dev/null; then exit 1; fi
    ' || fail 'Management resources are visible from the SSH sandbox.'

    printf 'PASS: SSH sandbox — no network, no observer process, no management credentials.\n'
  else
    rules=$(docker exec --user 0:0 "$container" iptables -S OUTPUT)
    expected=$(printf '%s\n%s' '-P OUTPUT DROP' '-A OUTPUT -m conntrack --ctstate ESTABLISHED --ctdir REPLY -j ACCEPT')
    [ "$rules" = "$expected" ] || fail 'Worker permits traffic beyond replies to inbound connections.'

    docker exec --user 65532:65532 "$container" /bin/sh -ec '
      test ! -e /var/run/docker.sock && test ! -d /run/secrets
      command -v timeout >/dev/null && command -v nslookup >/dev/null
      test "$(awk "/^CapEff:/ {print \$2}" /proc/$(pidof node-metrics)/status)" = 0000000000000000
    ' || fail 'Worker retained privileges or management resources.'

    if docker exec --user 65532:65532 "$container" timeout 2 nslookup service 127.0.0.11 >/dev/null 2>&1; then
      fail 'Worker can query Docker DNS.'
    fi

    printf 'PASS: service worker — replies only, DNS blocked, no management mounts, zero effective capabilities.\n'
  fi
done

printf 'PASS: observation stays outside the attacker sandbox; telemetry uses the external control plane.\n'
