# Video player

A Go web server that lists the MKV files in `VIDEO_DIR`, transcodes them to HLS with ffmpeg, and streams them to a browser player with switchable soundtracks.

## Requirements

- Go 1.27 or newer
- `ffmpeg` and `ffprobe` on your `PATH`

On macOS with Homebrew:

```bash
brew install go ffmpeg
```

## Run

From the project directory:

```bash
go run .
```

This compiles in memory and starts the server; no `videoplayer` binary is written. Stop it with `Ctrl+C`.

To use a different port or video folder:

```bash
PORT=8081 VIDEO_DIR=/path/to/videos go run .
```

`VIDEO_DIR` defaults to `/Users/fangming.ning/Documents/video`.

Then open:

- Player: http://localhost:8080/
- Controls: http://localhost:8080/controls

Keep the player page open; the controls page sends commands to it. Click once on the player page before using the controls, because browsers block autoplay with sound until the page has had a user gesture.

## Adding videos

Put `.mkv` files in the `VIDEO_DIR` folder (subfolders work too). Open http://localhost:8080/admin and choose **Load songs**. A file is stored only when its name is `Singer-Song-Language-Style.mkv`. Loading again adds new files and leaves ones already in `.data.db` (in `VIDEO_DIR`) as they are. **Clear library** removes every stored song. The Songs tab searches that database by singer and song name.

The first play of each file transcodes it into `.cache/`; later plays reuse that output until it is a day old. A background job runs at startup and then once a day, and deletes cache folders created more than 24 hours earlier. A folder whose transcode is still running is left alone.

## HTTP API

Commands are broadcast to every open player page. Each endpoint accepts `GET` or `POST`.

| Endpoint | Purpose |
| --- | --- |
| `/api/queue?file=test.mkv` | Add a song to the queue. Starts it immediately when nothing is playing |
| `/api/queue/remove?id=1` | Remove a queued song (`POST`) |
| `/api/next` | Skip to the next queued song, or stop when the queue is empty |
| `/api/play?file=test.mkv&track=0` | Load and play a file now; `track` is optional |
| `/api/pause` | Pause playback |
| `/api/resume` | Resume playback |
| `/api/track?track=1` | Switch soundtrack (0-based index) |
| `/api/state` | Current playback state (`GET`) |
| `/api/videos` | List available videos (`GET`) |

Example:

```bash
curl -X POST 'http://localhost:8080/api/play?file=test.mkv&track=1'
```
