#!/bin/sh
set -e

# Default upstream to the in-cluster server service if not overridden.
export OPENDEPOT_SERVER_HOST="${OPENDEPOT_SERVER_HOST:-server.opendepot-system.svc.cluster.local:80}"

# Substitute environment variables into the nginx template.
envsubst '${OPENDEPOT_SERVER_HOST}' < /etc/nginx/nginx.conf.template > /etc/nginx/nginx.conf

# Start Next.js standalone server in the background.
node /app/server.js &

# Start NGINX in the foreground (PID 1 equivalent).
exec nginx -g "daemon off;"
