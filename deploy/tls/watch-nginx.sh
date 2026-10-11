#!/bin/sh
# nginx's entrypoint sources executable hooks before starting the master.
set -eu
(
  previous=$(sha256sum /etc/nginx/certs/*.crt /etc/nginx/certs/*.key)
  while sleep 30; do
    current=$(sha256sum /etc/nginx/certs/*.crt /etc/nginx/certs/*.key)
    if [ "$current" != "$previous" ]; then
      if nginx -t && nginx -s reload; then previous=$current; fi
    fi
  done
) &
