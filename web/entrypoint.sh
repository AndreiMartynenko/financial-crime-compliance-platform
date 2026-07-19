#!/bin/sh
set -eu
envsubst '${OIDC_AUTHORITY} ${OIDC_CLIENT_ID}' < /opt/fccp/config.js.template > /usr/share/nginx/html/config.js
exec nginx -g 'daemon off;'
