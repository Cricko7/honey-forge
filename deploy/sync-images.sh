#!/usr/bin/env bash
set -euo pipefail

RELEASE_ENV=$(readlink -f "${1:?release.env is required}")
ARCHIVE=$(readlink -f "${2:?image archive is required}")
RELEASE_ID=${3:?release ID is required}
[[ "$RELEASE_ID" =~ ^[a-zA-Z0-9-]{1,24}$ ]] || { echo 'Invalid release ID' >&2; exit 1; }
test -r "$RELEASE_ENV"
test -r "$ARCHIVE"

local_node=$(docker info --format '{{.Swarm.NodeID}}')
test -n "$local_node" || { echo 'Image distribution requires a Swarm manager' >&2; exit 1; }
nodes=$(docker node ls --format '{{.ID}} {{.Status}} {{.Availability}}')
architecture=$(docker node inspect --format '{{.Description.Platform.Architecture}}' "$local_node")
remote_nodes=()
while read -r node status availability; do
  [[ "$node" =~ ^[a-zA-Z0-9]+$ ]] || { echo 'Invalid Swarm node ID' >&2; exit 1; }
  if [ "$status" != Ready ] || [ "$availability" != Active ]; then
    echo "Node $node is $status/$availability; restore readiness before deployment" >&2
    exit 1
  fi

  node_architecture=$(docker node inspect --format '{{.Description.Platform.Architecture}}' "$node")
  if [ "$node_architecture" != "$architecture" ]; then
    echo "Node $node requires a different image architecture" >&2
    exit 1
  fi

  if [ "$node" != "$local_node" ]; then remote_nodes+=("$node"); fi
done <<< "$nodes"

if [ "${#remote_nodes[@]}" -eq 0 ]; then
  echo 'All release images are already on the only Swarm node'
  exit 0
fi

expected_images=''
server_image=''
while IFS='=' read -r key image; do
  case "$key" in
    *_IMAGE)
      [[ "$image" =~ ^[a-zA-Z0-9_./:@-]+$ ]] || { echo 'Invalid release image reference' >&2; exit 1; }
      digest=$(docker image inspect --format '{{.Id}}' "$image")
      expected_images+="$image $digest"$'\n'
      if [ "$key" = CONSTRUCTOR_IMAGE ]; then server_image=$image; fi
      ;;
  esac
done < "$RELEASE_ENV"
test -n "$expected_images"
test -n "$server_image"
archive_hash=$(sha256sum "$ARCHIVE" | cut -d ' ' -f 1)

services=()
cleanup() {
  local result=$?
  trap - EXIT
  for ((index=${#services[@]}-1; index>=0; index--)); do
    if ! docker service rm "${services[index]}" >/dev/null 2>&1; then
      echo "Could not remove temporary service ${services[index]}" >&2
      result=1
    fi
  done
  exit "$result"
}
trap cleanup EXIT

source_service="hf-image-source-$RELEASE_ID"
services+=("$source_service")
docker service create --detach=true --no-resolve-image --name "$source_service" \
  --constraint "node.id==$local_node" --network hf-mgmt \
  --restart-condition none --read-only --cap-drop ALL \
  --limit-memory 128m --limit-cpu 1 \
  --mount "type=bind,source=$ARCHIVE,target=/srv/images.tar.gz,readonly" \
  --entrypoint python "$server_image" -m http.server 8000 --directory /srv >/dev/null

source_ready=0
for attempt in $(seq 1 60); do
  state=$(docker service ps --no-trunc --format '{{.CurrentState}}' "$source_service" | head -n 1)
  case "$state" in
    Running*) source_ready=1; break;;
    Failed*|Rejected*) echo 'Image source failed to start' >&2; exit 1;;
  esac
  sleep 2
done
test "$source_ready" -eq 1 || { echo 'Image source startup timed out' >&2; exit 1; }

loaders=()
for node in "${remote_nodes[@]}"; do
  # Existing trusted supervisors already contain the Docker CLI on enrolled workers.
  bootstrap=$(docker node ps --no-trunc --filter desired-state=running --format '{{.Image}} {{.CurrentState}}' "$node" |
    awk '$2 == "Running" && !image && /^honeyforge\/(tcp-decoy|redis-decoy|honeytoken-decoy|http-decoy|ssh-decoy):/ {image=$1} END {if (image) print image}')
  bootstrap=${bootstrap:-${DEPLOY_IMAGE_LOADER_IMAGE:-docker:27.5.1-cli}}
  loader="hf-image-sync-$RELEASE_ID-$node"
  services+=("$loader")
  loaders+=("$loader")
  docker service create --detach=true --no-resolve-image --name "$loader" \
    --mode replicated-job --replicas 1 --restart-condition none \
    --constraint "node.id==$node" --network hf-mgmt \
    --limit-memory 128m --limit-cpu 1 --cap-drop ALL \
    --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock \
    --env "ARCHIVE_URL=http://$source_service:8000/images.tar.gz" \
    --env "ARCHIVE_HASH=$archive_hash" --env "EXPECTED_IMAGES=$expected_images" \
    --entrypoint /bin/sh "$bootstrap" -ec '
      set -o pipefail
      archive=$(mktemp)
      trap '\''rm -f "$archive"'\'' EXIT
      wget -T 120 -q -O "$archive" "$ARCHIVE_URL"
      printf "%s  %s\n" "$ARCHIVE_HASH" "$archive" | sha256sum -c -
      gzip -dc "$archive" | docker image load
      printf "%s" "$EXPECTED_IMAGES" | while read -r image expected; do
        actual=$(docker image inspect --format "{{.Id}}" "$image")
        test "$actual" = "$expected" || { echo "Image verification failed: $image" >&2; exit 1; }
      done
      echo "Release images verified"
    ' >/dev/null
done

for attempt in $(seq 1 300); do
  completed=0
  for loader in "${loaders[@]}"; do
    state=$(docker service ps --no-trunc --format '{{.CurrentState}}' "$loader" | head -n 1)
    case "$state" in
      Complete*) completed=$((completed + 1));;
      Failed*|Rejected*|Orphaned*)
        echo "Image distribution failed: $loader ($state)" >&2
        docker service logs --tail=20 "$loader" >&2 || true
        exit 1
        ;;
    esac
  done
  if [ "$completed" -eq "${#loaders[@]}" ]; then
    echo "Release images verified on all $((completed + 1)) Swarm nodes"
    exit 0
  fi
  sleep 2
done
echo 'Image distribution timed out; existing applications were not switched' >&2
exit 1
