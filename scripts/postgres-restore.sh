#!/bin/sh
set -eu

: "${RESTORE_DATABASE_URL:?RESTORE_DATABASE_URL must be set}"
: "${1:?usage: postgres-restore.sh BACKUP_FILE}"

backup_path=$1
checksum_path="${backup_path}.sha256"

if [ "${CONFIRM_DATABASE_RESTORE:-}" != "restore" ]; then
  echo "Refusing destructive restore: set CONFIRM_DATABASE_RESTORE=restore" >&2
  exit 2
fi

if [ ! -f "$backup_path" ]; then
  echo "Backup does not exist: $backup_path" >&2
  exit 2
fi

if [ -f "$checksum_path" ]; then
  backup_dir=$(dirname "$backup_path")
  checksum_name=$(basename "$checksum_path")
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$backup_dir" && sha256sum -c "$checksum_name")
  else
    (cd "$backup_dir" && shasum -a 256 -c "$checksum_name")
  fi
else
  echo "Refusing unverified restore: missing $checksum_path" >&2
  exit 2
fi

pg_restore --list "$backup_path" >/dev/null
pg_restore \
  --dbname="$RESTORE_DATABASE_URL" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  --single-transaction \
  --exit-on-error \
  "$backup_path"
