#!/usr/bin/env bash
set -euo pipefail

ROOT=$(readlink -f "${DEPLOY_ROOT:-/opt/honeyforge}")
INCOMING="$ROOT/incoming"
test -d "$INCOMING"
CURRENT=$(readlink -f "$ROOT/current" 2>/dev/null || true)
docker_root=$(docker info --format '{{.DockerRootDir}}')
echo 'Disk usage before release artifact cleanup'
df -Pk "$ROOT" "$docker_root"

# Only archives produced by Actions; keep current release and all source/data files.
while IFS= read -r -d '' archive; do
  directory=$(dirname "$archive")
  name=$(basename "$directory")
  [[ "$name" =~ ^[0-9]+-[0-9]+$ ]] || continue
  directory=$(readlink -f "$directory")
  [[ "$directory" == "$INCOMING/"* ]] || exit 1
  test "$directory" != "$CURRENT" || continue
  rm -- "$archive"
done < <(find "$INCOMING" -mindepth 2 -maxdepth 2 -type f -name images.tar.gz -print0)

declare -A protected_ids=() protected_tags=()
protect() {
  local image=${1%@*} id
  test -n "$image" || return 0
  protected_tags["$image"]=1
  if id=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null); then
    protected_ids["$id"]=1
  fi
}
while IFS= read -r image; do protect "$image"; done < <(docker ps -a --format '{{.Image}}')
while IFS= read -r image; do protect "$image"; done < <(docker service ls --format '{{.Image}}' 2>/dev/null || true)
if [ -f "$CURRENT/release.env" ]; then
  while IFS='=' read -r key image; do
    [[ "$key" == *_IMAGE ]] || continue
    protect "$image"
  done < "$CURRENT/release.env"
fi

while read -r repository tag id; do
  case "$repository" in
    honeyforge/api|honeyforge/orchestrator|honeyforge/constructor|honeyforge/tcp-decoy|honeyforge/redis-decoy|honeyforge/honeytoken-decoy|honeyforge/http-decoy|honeyforge/ssh-decoy|honeyforge/ssh-sandbox) ;;
    *) continue ;;
  esac
  test "$tag" != '<none>' || continue
  [ -z "${protected_ids[$id]:-}" ] || continue
  [ -z "${protected_tags[$repository:$tag]:-}" ] || continue
  # Never force removal: Docker also protects images referenced by containers.
  docker image rm "$repository:$tag" || true
done < <(docker image ls --no-trunc --format '{{.Repository}} {{.Tag}} {{.ID}}')

echo 'Disk usage after release artifact cleanup'
df -Pk "$ROOT" "$docker_root"
