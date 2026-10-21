# Backup and restore

Your data is two things:

1. the **SQLite database** (`TRAINING_DB_PATH`, `/data/training.db` in Docker), and
2. **uploaded videos** in `MEDIA_DIR` (default: `media/` beside the database,
   `/data/media` in Docker). Only video files live there; metadata is in the
   database.

A complete backup needs both. `backup` snapshots **only the database**.

## Back up

The built-in command takes a consistent database snapshot with `VACUUM INTO`. It
is safe while the server is running, creates the destination folder if needed,
and **refuses to overwrite an existing file** (use a new name each time):

```sh
training-mcp backup ./backups/training-$(date +%F).db
# Docker (the destination must not exist yet):
docker exec <container> training-mcp backup /data/backups/training-$(date +%F).db
docker cp <container>:/data/backups/training-$(date +%F).db ./backups/
```

Copy the media directory too. Video files are written once and never modified,
so copying it while the server runs is safe:

```sh
rsync -a "$MEDIA_DIR"/ ./backups/media/        # or: docker cp <container>:/data/media ./backups/media
```

Verify the database copy if you have the `sqlite3` CLI:

```sh
sqlite3 ./backups/training-2026-03-10.db 'PRAGMA integrity_check;'   # prints: ok
```

Schedule it (cron, a systemd timer, your host's scheduler) — a backup you must
remember to take is not a backup. Keep several generations and store copies off
the machine. Backups contain your full training history and videos and are as
sensitive as the database: do not commit them (`*.db` is git-ignored).

Do **not** copy the live `.db` file with `cp` while the server is running: in
WAL mode recent data may still be in the `-wal` file.

## Restore

1. Stop the server.
2. Remove the current `training.db`, `training.db-wal` and `training.db-shm`
   (or move them aside).
3. Copy the database backup to `TRAINING_DB_PATH`.
4. Restore the media directory from the same backup into `MEDIA_DIR`. If you skip
   this, the videos' rows survive but the files are missing: playback returns
   404 "video file is missing" until you restore them.
5. Start the server. Migrations apply if the backup is from an older version.

Restoring replaces everything written since the backup, including idempotency
keys. `training-mcp media-gc` removes files no database row refers to (for
example after restoring an older database over a newer media directory).

## Plan export is not a backup

`export_plan` / `GET /plans/{id}/export` exports routines only (no history, no
videos). Use it to share or version-control plans.
