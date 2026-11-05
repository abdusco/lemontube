# LemonTube

LemonTube is a self-hosted download queue powered by yt-dlp and ffmpeg. Add a video or playlist URL, choose a format, and follow the job in the browser. Subtitle languages and Netscape-format cookies are supported.

yt-dlp embeds available metadata and chapters in downloaded audio and video using ffmpeg. Requested subtitles are saved separately; thumbnails are not embedded.

```sh
docker compose up --build -d
```

Open http://localhost:8081. The Compose file stores state in `./data` and downloads in `./downloads`.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8081` | HTTP listen address |
| `STATE_DIR` | `.` | Database and instance lock directory |
| `DOWNLOAD_DIR` | `downloads` | Published downloads |
| `TEMP_DIR` | `DOWNLOAD_DIR` | Parent of LemonTube's private workspaces |
| `YTDLP_PATH` | unset | Use an externally managed yt-dlp executable instead of the built-in update mechanism |
| `MAX_CONCURRENT_DOWNLOADS` | `3` | Concurrent download limit |
| `DELETE_FILE_ON_TRASHCAN` | `false` | Delete unchanged published outputs when removing a job |
| `CLEAR_COMPLETED_AFTER` | `0` | Automatically remove finished jobs after this many seconds; zero disables expiry |
| `DEBUG` | `0` | With `1`, serve frontend assets from `./web` |

Only one instance may use a state directory. Place LemonTube behind an authenticated reverse proxy if other users can reach it; the application does not currently provide authentication. Cookies are stored in the database and should be protected like passwords.

The image includes a uv-installed yt-dlp as its initial system copy. The **yt-dlp** section shows its version and can install the latest standalone nightly build into `STATE_DIR/bin/yt-dlp`. That managed copy persists with the state volume and takes precedence on subsequent runs. Updates wait for active downloads before replacing the binary. If `YTDLP_PATH` is set, LemonTube uses that executable and disables its update button; maintain it externally.

Every download uses an isolated workspace. Temporary files are removed after the job and checked again on startup and periodically. Existing files in the download directory are never overwritten. When output deletion is enabled, LemonTube only removes recorded files whose contents still match the published output; changed files are retained for manual review.

When a job is removed without deleting its files, LemonTube keeps a hidden record of its outputs. The settings menu can clear these files later if they are unchanged. Files from jobs removed before this tracking was introduced cannot be identified reliably and are left untouched.

## Development

Install Go, yt-dlp, ffmpeg, and a supported JavaScript runtime for yt-dlp. Run `DEBUG=1 go run ./cmd/lemontube` and `go test ./...`. yt-dlp must be kept up to date as supported sites change.
