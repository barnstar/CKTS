# CKTS Radio

A streaming audio server written in pure Go for Raspberry Pi and macOS. Broadcasts MP3 playlists or live line-in audio to multiple browser clients over a [Tailscale](https://tailscale.com) network using [tsnet](https://pkg.go.dev/tailscale.com/tsnet).

## Features

- Stream MP3 files from a playlist (loops continuously)
- Stream live audio from a line-in device (Linux ALSA or macOS AVFoundation)
- Multiple simultaneous listeners — all hear the same broadcast
- Simple web interface: live status, current track, listener count, play/stop button
- Pure Go binary, no CGo — cross-compiles cleanly for `linux/arm64` (Raspberry Pi)
- Runs as a named Tailscale node; no open ports needed

## Requirements

- Go 1.25.5 or later (`go mod tidy` will download the right toolchain automatically)
- A Tailscale account (or use `-local` for LAN-only operation)
- `ffmpeg` installed if using line-in (see [Installing dependencies](#installing-dependencies))

## Installing dependencies

Line-in capture requires `ffmpeg` with the `libmp3lame` MP3 encoder.

### Debian / Ubuntu / Raspberry Pi OS

```bash
sudo apt update
sudo apt install ffmpeg alsa-utils
```

`alsa-utils` provides `arecord -l` and `amixer` for listing and configuring ALSA devices. `ffmpeg` handles the actual audio capture and MP3 encoding.

### macOS (Homebrew)

```bash
brew install ffmpeg
```

The macOS build of ffmpeg includes AVFoundation support and libmp3lame by default.

### Verify installation

```bash
ffmpeg -version          # should show version and enabled encoders
ffmpeg -encoders 2>/dev/null | grep mp3lame   # confirm libmp3lame is available
```

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
| `-linein` | | Stream from audio line-in instead of a file |
| `-device <name>` | `default` (Linux) / `0` (macOS) | Audio capture device (Linux ALSA: e.g. `hw:3,0`; macOS AVFoundation: device index e.g. `0`) |
| `-callsign <name>` | `CKTS` | Radio station callsign (single word, no spaces). Displayed in the web UI as "*callsign* Radio" and used as the Tailscale hostname (`<callsign>-Radio`) |
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

**Stream line-in over Tailscale (Linux):**
```bash
./ckts -linein -device hw:3,0 -authkey tskey-auth-xxxx -autoplay
```

**Stream line-in (macOS):**
```bash
./ckts -linein -device 0 -local -autoplay
```

**Local network only (no Tailscale), for testing:**
```bash
./ckts -local -addr :8080 -playlist /home/pi/music/playlist.txt
```

**Custom callsign — brand your station "WKRP Radio":**
```bash
./ckts -callsign WKRP -playlist /home/pi/music/playlist.txt -autoplay
```
The web UI will display "WKRP Radio" and the Tailscale node will appear as `WKRP-Radio` on your network.

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
| Line-in | `audio/mpeg` | Live MP3 encoded by ffmpeg (192 kbps, 44.1 kHz stereo) |

MP3 files are piped directly to clients without decoding, preserving the original quality and minimising CPU use on the Pi. Line-in audio is captured via `ffmpeg` (using ALSA on Linux or AVFoundation on macOS), encoded to MP3 in real-time, and streamed to all connected clients.

## Finding your audio capture device

### Linux (ALSA)

List capture devices:

```bash
arecord -l
```

Example output:
```
card 3: HLMSC4 [CUBILUX HLMS-C4], device 0: USB Audio [USB Audio]
```

Use `hw:<card>,<device>` as the `-device` value, e.g. `-device hw:3,0`. If the device doesn't natively support the requested sample rate, use `plughw:<card>,<device>` instead (e.g. `-device plughw:3,0`) which adds ALSA's software format conversion.

#### Selecting line-in vs microphone

Some USB audio interfaces expose line-in and microphone as mixer switches on the same device. Use `amixer` to check and switch:

```bash
# List all mixer controls on card 3
amixer -c 3 contents

# Switch from mic to line-in
amixer -c 3 cset name='Mic Capture Switch' off
amixer -c 3 cset name='Line Capture Switch' on
amixer -c 3 cset name='Line Capture Volume' 47,47
```

#### Test a device directly

```bash
arecord -D hw:3,0 -f S16_LE -c 2 -r 44100 -d 5 test.wav
aplay test.wav
```

### macOS (AVFoundation)

List available audio devices:

```bash
ffmpeg -f avfoundation -list_devices true -i ""
```

The device index (e.g. `0`, `1`) is used as the `-device` value. Only the audio device index is needed — CKTS passes it to ffmpeg as `:index` (audio-only).

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
- The **line-in source** spawns `ffmpeg` as a child process, capturing audio from the OS audio subsystem (ALSA on Linux, AVFoundation on macOS) and encoding it to MP3 on stdout. The MP3 stream is read and broadcast to all connected clients in real-time.
- tsnet state (Tailscale keys, etc.) is stored in the current working directory under `tsnet-state/`. Run the server from a persistent directory or set `TS_AUTHKEY` if you want fully unattended operation.
