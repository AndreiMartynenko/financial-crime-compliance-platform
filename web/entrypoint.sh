#!/bin/sh
set -eu
envsubst '${OIDC_AUTHORITY} ${OIDC_CLIENT_ID}' < /opt/fccp/config.js.template > /tmp/config.js
exec nginx -g 'daemon off;'
