#!/usr/bin/env bash
# Nightly SQLite backup of the API volume. Install on the host:
#   (crontab -l; echo "17 2 * * * bash $HOME/helderberg-social/api/backup.sh") | crontab -
# Keeps 30 daily copies under ~/backups/helderberg-social. Uses SQLite's own
# online backup through a throwaway container so the live file is never
# copied mid-write.
set -euo pipefail
DEST="${HS_BACKUP_DIR:-$HOME/backups/helderberg-social}"
mkdir -p "$DEST"
STAMP=$(date +%Y%m%d)
docker run --rm -v helderberg-social_hs-data:/data:ro -v "$DEST":/out alpine:3.20 sh -c \
  'apk add --no-cache sqlite >/dev/null && sqlite3 /data/helderberg.sqlite ".backup /out/helderberg-'"$STAMP"'.sqlite"'
gzip -f "$DEST/helderberg-$STAMP.sqlite"
find "$DEST" -name 'helderberg-*.sqlite.gz' -mtime +30 -delete
echo "backup written: $DEST/helderberg-$STAMP.sqlite.gz ($(du -h "$DEST/helderberg-$STAMP.sqlite.gz" | cut -f1))"
