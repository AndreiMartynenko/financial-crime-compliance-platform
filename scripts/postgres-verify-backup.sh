#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL must be set}"
: "${1:?usage: postgres-verify-backup.sh BACKUP_FILE}"

backup_path=$1
verify_database="fccp_restore_verify_$(date -u +%Y%m%d%H%M%S)_$$"
url_without_query=${DATABASE_URL%%\?*}
server_url=${url_without_query%/*}

case "$DATABASE_URL" in
  *\?*) verify_url="${server_url}/${verify_database}?${DATABASE_URL#*\?}" ;;
  *) verify_url="${server_url}/${verify_database}" ;;
esac

cleanup() {
  dropdb --if-exists --force --maintenance-db="$DATABASE_URL" "$verify_database" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

createdb --maintenance-db="$DATABASE_URL" "$verify_database"
RESTORE_DATABASE_URL="$verify_url" CONFIRM_DATABASE_RESTORE=restore \
  "$(dirname "$0")/postgres-restore.sh" "$backup_path"

psql "$verify_url" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --command="
  SELECT CASE
    WHEN to_regclass('public.customers') IS NOT NULL
     AND to_regclass('public.audit_events') IS NOT NULL
     AND (SELECT count(*) FROM schema_migrations) > 0
    THEN 'restore verification: ok'
    ELSE 'restore verification: failed'
  END;
" | grep -q 'restore verification: ok'

echo "Backup restored and verified in disposable database $verify_database"
