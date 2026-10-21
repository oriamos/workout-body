# Backup and restore

All data is one SQLite file (`TRAINING_DB_PATH`, `/data/training.db` in Docker).

## Back up

The built-in command takes a consistent snapshot with `VACUUM INTO`. It is safe
while the server is running:

```sh
training-mcp backup ./backups/training-$(date +%F).db
# Docker:
docker exec <container> training-mcp backup /data/backup.db   # then copy it out
docker cp <container>:/data/backup.db ./backups/training-$(date +%F).db
```

The result is a plain SQLite file. Verify it if you have the `sqlite3` CLI:

```sh
sqlite3 ./backups/training-2026-03-10.db 'PRAGMA integrity_check;'   # prints: ok
```

Schedule it (cron, systemd timer, your host's scheduler) — a backup you must
remember to take is not a backup. Keep several generations, and store copies
off the machine. Backups contain your full training history and are as
sensitive as the database: do not commit them (`*.db` is git-ignored).

Do **not** copy the live `.db` file with `cp` while the server is running: in
WAL mode the data may still be in the `-wal` file.

## Restore

1. Stop the server.
2. Remove the current `training.db`, `training.db-wal` and `training.db-shm`
   (or move them aside).
3. Copy the backup to `TRAINING_DB_PATH`.
4. Start the server. Migrations apply if the backup is from an older version.

Restoring replaces everything written since the backup, including idempotency
keys.

## Plan export is not a backup

`export_plan` / `GET /plans/{id}/export` exports routines only (no history).
Use it to share or version-control plans.
