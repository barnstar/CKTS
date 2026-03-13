package main

import (
	"context"
	"log"
	"os/exec"
	"sync"
)

// LineInSource captures audio from an ALSA device using ffmpeg and
// streams it to clients as MP3 (audio/mpeg).
type LineInSource struct {
	device string
	hub    *Hub
	mu     sync.Mutex
	active bool
	cancel context.CancelFunc
}

func NewLineInSource(device string, hub *Hub) *LineInSource {
	return &LineInSource{device: device, hub: hub}
}

func (l *LineInSource) CurrentTrack() string {
	return "Line-in (" + l.device + ")"
}

func (l *LineInSource) IsPlaying() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

func (l *LineInSource) Start() error {
	// Stream as MP3 so browsers can decode it incrementally.
	// WAV is not a streaming format and causes clicks/pops.
	l.hub.SetFormat("audio/mpeg", nil)

	ctx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.cancel = cancel
	l.active = true
	l.mu.Unlock()

	go l.capture(ctx)
	return nil
}

func (l *LineInSource) Stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.active = false
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	l.hub.CloseAll()
}

func (l *LineInSource) capture(ctx context.Context) {
	// Use ffmpeg to capture from ALSA and encode to MP3 in real-time.
	// MP3 is frame-based so browsers can decode the stream incrementally,
	// unlike WAV which is a file format and causes clicks/pops when streamed.
	//
	// Audio format options (-ac, -ar) are placed AFTER -i so they apply to
	// the output encoder.  This lets ffmpeg auto-negotiate the native ALSA
	// capture format and resample internally, avoiding garbled audio when
	// the device doesn't natively support the requested rate.
	cmd := exec.CommandContext(ctx,
		"ffmpeg",
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "alsa",
		"-i", l.device,
		"-ac", "2",
		"-ar", "44100",
		"-codec:a", "libmp3lame",
		"-b:a", "192k",
		"-write_xing", "0",
		"-flush_packets", "1",
		"-f", "mp3",
		"pipe:1",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("linein: pipe error: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("linein: ffmpeg start error: %v", err)
		log.Printf("linein: ensure ffmpeg is installed (apt install ffmpeg)")
		return
	}

	log.Printf("linein: capturing from device %q", l.device)

	buf := make([]byte, 8192)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			// Copy the slice so the hub can hand it off safely.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			l.hub.Broadcast(chunk)
		}
		if err != nil {
			break
		}
	}

	_ = cmd.Wait()
	log.Printf("linein: capture exited")
}
