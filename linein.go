package main

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"os/exec"
	"sync"
)

const (
	sampleRate    = 44100
	channels      = 2
	bitsPerSample = 16
)

// LineInSource captures audio from an ALSA device using arecord and
// streams it to clients as a WAV stream (audio/wav).
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
	header := buildWAVHeader()
	l.hub.SetFormat("audio/wav", header)

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
	// arecord outputs raw S16_LE PCM; we wrap it in WAV on the client side
	// by sending a WAV header to each new subscriber (via hub.SetFormat).
	cmd := exec.CommandContext(ctx,
		"arecord",
		"-D", l.device,
		"-f", "S16_LE",
		"-c", "2",
		"-r", "44100",
		"-t", "raw",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("linein: pipe error: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("linein: arecord start error: %v", err)
		log.Printf("linein: ensure alsa-utils is installed (apt install alsa-utils)")
		return
	}

	log.Printf("linein: capturing from device %q", l.device)

	const chunkSize = 4096
	buf := make([]byte, chunkSize)
	for {
		n, err := io.ReadFull(stdout, buf)
		if n > 0 {
			l.hub.Broadcast(buf[:n])
		}
		if err != nil {
			break
		}
	}

	_ = cmd.Wait()
	log.Printf("linein: arecord exited")
}

// buildWAVHeader returns a 44-byte WAV header for a streaming PCM source.
// The data-chunk size is set to 0xFFFFFFFF to signal an open-ended stream.
func buildWAVHeader() []byte {
	const dataSize = 0xFFFFFFFF
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	blockAlign := uint16(channels * bitsPerSample / 8)

	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], 0xFFFFFFFF) // open-ended stream
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:], 1)  // PCM = 1
	binary.LittleEndian.PutUint16(hdr[22:], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:], uint16(bitsPerSample))
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], dataSize)
	return hdr
}
