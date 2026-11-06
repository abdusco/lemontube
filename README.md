# LemonTube

LemonTube is a personal download manager for videos, playlists, and audio. Add a link, choose the format and quality you want, and follow downloads in your browser.

## Downloading

1. Paste a video or playlist link.
2. Choose video or audio, a format, and a quality. Optionally, enter subtitle languages.
3. Select **Download**. Your downloads and their progress appear below the form.

Finished files are available through their links in the downloads list and in your downloads folder. Available metadata and chapters are included in the media file. Requested subtitles are saved as separate files; thumbnails are not embedded.

Open a download's **Log** for details, or retry a failed or canceled download.

## Site cookies

Some sites require you to sign in before downloading. Expand **Site cookies** and upload or paste a Netscape-format `cookies.txt` file. You can export one using [Get cookies.txt LOCALLY for Chrome](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc).

Saved domains are listed individually, so you can remove cookies for one site or delete them all. Treat cookie files and LemonTube's stored data like passwords: they can grant access to your accounts.

## Managing downloads

Removing a download from the list **keeps its files by default**. Open the settings menu and select **Clear orphaned files** to delete unchanged files from removed downloads. Files LemonTube can't identify or that have changed are left alone. Existing files are never overwritten.

The settings menu also lets you update yt-dlp when downloads stop working. Running downloads are not interrupted.

## Installation

Install Docker and the Docker Compose plugin, then download the [Compose file](https://raw.githubusercontent.com/abdusco/lemontube/master/compose.yaml) into a folder and run:

```sh
docker compose up -d
```

Open [localhost:8081](http://localhost:8081). Downloads are saved in `./downloads` and application data in `./data`, relative to the Compose file. Back up `./data` if you want to keep your download history and settings.

By default, LemonTube has no login. To require one, set `AUTH_USERS` to a JSON array of `username:password` strings before starting it. For example:

```sh
export AUTH_USERS='["alice:replace-with-a-strong-password","bob:another-strong-password"]'
docker compose up -d
```

Keep these credentials private: they are stored as plaintext in the environment. Use HTTPS through a reverse proxy when accessing LemonTube over a network, and restrict access to trusted users. Sign-in sessions expire after 24 hours or a server restart. If `AUTH_USERS` is unset, anyone who can reach LemonTube has access; an empty or invalid value prevents it from starting.

### Configuration

Set environment variables in the Compose file to change these defaults:

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAX_CONCURRENT_DOWNLOADS` | `3` | Number of downloads that can run at once |
| `DELETE_FILE_ON_TRASHCAN` | `false` | Delete unchanged files when removing a download |
| `CLEAR_COMPLETED_AFTER` | `0` | Automatically remove finished downloads after this many seconds; `0` disables this |
| `YTDLP_PATH` | unset | Use an externally managed yt-dlp executable instead of in-app updates |
| `AUTH_USERS` | unset | JSON array of login credentials; unset disables login |

Only one LemonTube instance should use the same data directory.
