#!/bin/sh
set -eu
umask 077
mkdir -p /home/sandbox/.ssh
printf '%s\n' "$SANDBOX_PUBLIC_KEY" > /home/sandbox/.ssh/authorized_keys
unset SANDBOX_PUBLIC_KEY
ssh-keygen -q -t ed25519 -N '' -f /home/sandbox/.ssh/host_key
printf 'ready\n' > /home/sandbox/.ssh/ready
exec sleep "$1"
