#!/bin/sh
set -eu
umask 077

: "${DATABASE_URL:?DATABASE_URL must be set}"

backup_dir=${BACKUP_DIR:-./backups}
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_name="fccp-${timestamp}.dump"
backup_path="${backup_dir}/${backup_name}"
temporary_path="${backup_path}.partial"

mkdir -p "$backup_dir"
if [ -e "$backup_path" ] || [ -e "${backup_path}.sha256" ]; then
  echo "Refusing to overwrite existing backup: $backup_path" >&2
  exit 2
fi
trap 'rm -f "$temporary_path"' EXIT HUP INT TERM

pg_dump \
  --dbname="$DATABASE_URL" \
  --format=custom \
  --compress=9 \
  --no-owner \
  --no-privileges \
  --file="$temporary_path"

pg_restore --list "$temporary_path" >/dev/null
mv "$temporary_path" "$backup_path"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$backup_dir" && sha256sum "$backup_name" > "${backup_name}.sha256")
else
  (cd "$backup_dir" && shasum -a 256 "$backup_name" > "${backup_name}.sha256")
fi

printf '%s\n' "$backup_path"
