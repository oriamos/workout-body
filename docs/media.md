# Videos and exercise how-to

Two things can be attached, both through the engine so MCP and HTTP share the
rules:

* a **video of a performed set** (a form check, a PR attempt), and
* **instructions for an exercise**: a written explanation plus one or more
  how-to videos.

Nothing here is a UI. Upload and playback are HTTP API operations for the future
frontend; the conversational assistant can read and edit the *text* and manage
video metadata, but never sees video content.

## Storage model

| What | Where |
|---|---|
| Video bytes | Files in `MEDIA_DIR` (default: `media/` next to the database file), named by a random key. |
| Metadata (id, owner, filename, content type, size, SHA-256, caption) | SQLite table `media` (migration 011). |
| Exercise explanation | `exercises.instructions` (≤ 5000 characters). Distinct from the short *setup note* (seat height, grip) that upstream already had. |

Bytes stay out of the database so backups of the `.db` stay small and fast.
**A full backup therefore needs both the database snapshot and the media
directory** — see [backup-restore.md](backup-restore.md).

Deleting a set (or its workout) deletes its videos' rows (cascade) and their
files. A file left behind after a crash (written, but its row never recorded) is
harmless and is removed by `training-mcp media-gc`, which only touches files
older than one hour (so a file whose row is about to commit is never collected)
and also clears abandoned upload temp files.

## Rules

| Rule | Value |
|---|---|
| Accepted containers | MP4 (known video brands such as `isom`, `mp42`, `avc1`), MOV (QuickTime), WebM — **identified from the file's bytes**; other ISO-media files such as HEIC/AVIF photos and non-WebM Matroska are refused. The declared `Content-Type` and the file extension are not trusted, so a script renamed `x.mp4` is rejected. |
| Maximum size | `MAX_VIDEO_MB` (default 200). A file exactly at the limit is accepted, one byte over is refused with HTTP 413 and leaves nothing on disk. |
| Per-owner limit | 5 videos per set, 10 per exercise (HTTP 409 beyond that). |
| File names | Only the base name is kept (no path components), control characters removed, ≤ 120 characters. Storage keys are random, so names never touch the filesystem. |
| Captions | ≤ 200 characters. |
| Disabled | If `MEDIA_DIR` cannot be created at startup the server logs a warning and keeps running with **video storage disabled**: video endpoints return HTTP 501 `media_disabled`, while workout logging, exercise instructions text and guide reads keep working. |

## Operations

| Operation | HTTP | MCP |
|---|---|---|
| Upload a set video | `POST /api/v1/sets/{id}/videos` (raw bytes or multipart) | — (not practical through a conversation) |
| Upload an exercise how-to video | `POST /api/v1/exercises/{ref}/videos` | — |
| List videos | `GET /api/v1/sets/{id}/videos`, `GET /api/v1/exercises/{ref}/videos` | `list_set_videos`, `get_exercise_guide` |
| Stream a video (HTTP Range supported) | `GET /api/v1/media/{id}` | — |
| Delete a video | `DELETE /api/v1/media/{id}` | `delete_video` |
| Read an exercise guide (text + videos) | `GET /api/v1/exercises/{ref}/guide` | `get_exercise_guide` |
| Write exercise instructions | `PUT /api/v1/exercises/{ref}/instructions` | `set_exercise_instructions` |

`{ref}` is an exercise id, name or alias. Request/response schemas:
[http-api.md](http-api.md) and the OpenAPI spec.

### Upload examples

```sh
# raw body
curl -X POST "$URL/api/v1/sets/12/videos?filename=bench-set-2.mp4&caption=Last+rep+slowed" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: video/mp4" \
  --data-binary @bench-set-2.mp4

# multipart
curl -X POST "$URL/api/v1/exercises/bench%20press/videos" \
  -H "Authorization: Bearer $TOKEN" \
  -F caption="Setup and bar path" -F file=@howto.mp4
```

## Limitations

* No transcoding, thumbnails or streaming ladders: a file is stored and served
  as uploaded. Whether a phone's HEVC `.mov` plays depends on the viewing
  device.
* Playback needs the bearer token. A bare `<video src>` tag cannot send it, so a
  future UI should fetch through its own authenticated layer (for example a
  same-origin proxy that adds the token) — media URLs are deliberately not
  public or pre-signed.
* The assistant cannot watch videos; it only knows they exist (and their
  captions, sizes and ids).
* Uploads are not resumable.
