# CKTS Radio

A streaming audio server written in pure Go for Raspberry Pi. Broadcasts MP3 playlists or live line-in audio to multiple browser clients over a [Tailscale](https://tailscale.com) network using [tsnet](https://pkg.go.dev/tailscale.com/tsnet).

## Features

- Stream MP3 files from a playlist (loops continuously)
- Stream live audio from any ALSA line-in device
- Multiple simultaneous listeners — all hear the same broadcast
- Simple web interface: live status, current track, listener count, play/stop button
- Pure Go binary, no CGo — cross-compiles cleanly for `linux/arm64` (Raspberry Pi)
- Runs as a named Tailscale node; no open ports needed

## Requirements

- Go 1.25.5 or later (`go mod tidy` will download the right toolchain automatically)
- A Tailscale account (or use `-local` for LAN-only operation)
- `alsa-utils` installed on the Pi if using line-in (`apt install alsa-utils`)

## Building

```bash
git clone <repo>
cd ckts
go mod tidy   # downloads dependencies and the correct Go toolchain
go build -o ckts .
```

Cross-compile for Raspberry Pi (64-bit):

```bash
GOOS=linux GOARCH=arm64 go build -o ckts-arm64 .
```

## Usage

```
./ckts [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-playlist <file>` | | Newline-separated list of MP3 file paths to stream |
| `-linein` | | Stream from ALSA line-in instead of a file |
| `-alsa-device <name>` | `default` | ALSA capture device (e.g. `hw:1,0`) |
| `-hostname <name>` | `CKTS-Radio` | Tailscale hostname for this node |
| `-authkey <key>` | | Tailscale auth key (omit to authenticate interactively) |
| `-autoplay` | | Begin streaming immediately on startup |
| `-local` | | Listen on a plain TCP address instead of tsnet |
| `-addr <addr>` | `:8080` | Listen address when using `-local` |

Exactly one of `-playlist` or `-linein` must be provided.

### Examples

**Stream a playlist over Tailscale:**
```bash
./ckts -playlist /home/pi/music/playlist.txt -autoplay
```

**Stream line-in over Tailscale with a specific auth key:**
```bash
./ckts -linein -alsa-device hw:1,0 -authkey tskey-auth-xxxx -autoplay
```

**Local network only (no Tailscale), for testing:**
```bash
./ckts -local -addr :8080 -playlist /home/pi/music/playlist.txt
```

**Custom Tailscale hostname:**
```bash
./ckts -hostname basement-radio -playlist /home/pi/music/playlist.txt -autoplay
```

### Playlist file format

One absolute file path per line. Lines beginning with `#` and blank lines are ignored.

```
# My playlist
/home/pi/music/01-track.mp3
/home/pi/music/02-track.mp3
/home/pi/music/03-track.mp3
```

## Listening

Once running, open a browser and navigate to:

- **Tailscale:** `http://CKTS-Radio` (or whatever `-hostname` you set)
- **Local:** `http://<pi-ip>:8080`

The web interface shows the current track (or "Line-in"), listener count, and a Start/Stop button. The audio player connects to the `/stream` endpoint automatically when you press Start.

You can also point any audio player directly at the stream URL:

```
http://CKTS-Radio/stream
```

## Audio format

| Source | Content-Type | Format |
|--------|-------------|--------|
| Playlist | `audio/mpeg` | Raw MP3 passthrough — no transcoding |
| Line-in | `audio/wav` | 44.1 kHz, 16-bit, stereo PCM with streaming WAV header |

MP3 files are piped directly to clients without decoding, preserving the original quality and minimising CPU use on the Pi. Line-in audio is captured via `arecord` (part of `alsa-utils`) and delivered as a streaming WAV — each new client receives a fresh WAV header followed by live PCM data.

## Finding your ALSA device

List capture devices:

```bash
arecord -l
```

Example output:
```
card 1: Device [USB Audio Device], device 0: USB Audio [USB Audio]
```

Use `hw:<card>,<device>` as the `-alsa-device` value, e.g. `-alsa-device hw:1,0`.

Test a device directly:

```bash
arecord -D hw:1,0 -f S16_LE -c 2 -r 44100 -d 5 test.wav
```

## Running as a systemd service

Create `/etc/systemd/system/ckts.service`:

```ini
[Unit]
Description=CKTS Radio
After=network.target

[Service]
ExecStart=/home/pi/ckts -playlist /home/pi/music/playlist.txt -autoplay
Restart=on-failure
User=pi
WorkingDirectory=/home/pi

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ckts
sudo journalctl -u ckts -f
```

## Architecture notes

- The **hub** is a thread-safe broadcaster. When the source produces a chunk, it is copied into each connected client's buffered channel. Slow clients get chunks dropped rather than blocking the broadcast — acceptable for radio-style streaming.
- The **playlist source** throttles reads to approximately 320 kbps so that all listeners stay roughly in sync (radio behaviour). Files play in order and the playlist loops indefinitely.
- The **line-in source** spawns `arecord` as a child process and reads its stdout. The WAV header is stored in the hub and sent to every new subscriber before they receive live PCM data.
- tsnet state (Tailscale keys, etc.) is stored in the current working directory under `tsnet-state/`. Run the server from a persistent directory or set `TS_AUTHKEY` if you want fully unattended operation.
