#!/bin/sh
# Read-only checks on the SAME Linux Docker node as the SSH gateway/sandboxes.
set -eu

[ "$(uname -s)" = Linux ] || { echo 'Tetragon requires a Linux Docker host' >&2; exit 1; }
command -v docker >/dev/null || { echo 'Docker CLI is required' >&2; exit 1; }
[ -r /sys/kernel/btf/vmlinux ] || { echo 'Kernel BTF is absent; review Tetragon kernel/BTF requirements' >&2; exit 1; }
printf 'Kernel: %s\n' "$(uname -r)"
docker version --format 'Docker server: {{.Server.Version}} / {{.Server.Os}}'
docker info --format 'Node: {{.Name}}; cgroup: {{.CgroupVersion}}; security: {{json .SecurityOptions}}'
printf '%s\n' 'BTF is readable. This is NOT proof that eBPF loading or container attribution works.'

if [ -n "${TETRAGON_CONTAINER:-}" ]; then
    case "$TETRAGON_CONTAINER" in
        -*|*[!a-zA-Z0-9_.-]*) echo 'Invalid TETRAGON_CONTAINER name' >&2; exit 1 ;;
    esac
    docker inspect --type container --format 'Sensor: {{.Config.Image}}; running: {{.State.Running}}; privileged: {{.HostConfig.Privileged}}; network: {{.HostConfig.NetworkMode}}' "$TETRAGON_CONTAINER"
fi
