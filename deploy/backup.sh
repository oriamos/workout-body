#!/bin/sh
# Daily backup: a consistent SQLite snapshot (taken by the running server) plus
# the uploaded videos, kept for 30 days. Run as root from cron, e.g.
#   15 3 * * * /srv/workout-body/deploy/backup.sh >> /var/log/workout-body-backup.log 2>&1
set -eu
here=$(cd "$(dirname "$0")" && pwd)
dest="$here/data/backups"
stamp=$(date +%Y-%m-%d-%H%M)
mkdir -p "$dest"
chown 65532:65532 "$dest"
# The snapshot is written inside the container (VACUUM INTO, safe while running).
docker exec workout-body /usr/local/bin/training-mcp backup "/data/backups/training-$stamp.db"
if [ -d "$here/data/media" ]; then
  tar -C "$here/data" -czf "$dest/media-$stamp.tar.gz" media
fi
find "$dest" -type f \( -name 'training-*.db' -o -name 'media-*.tar.gz' \) -mtime +30 -delete
echo "$(date -Is) backup ok: $dest/training-$stamp.db"
