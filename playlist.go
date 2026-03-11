package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// PlaylistSource streams MP3 files listed in a playlist file.
// Files are streamed as raw bytes with Content-Type audio/mpeg.
// The playlist loops indefinitely until Stop is called.
type PlaylistSource struct {
	path   string
	hub    *Hub
	mu     sync.Mutex
	track  string
	active bool
	cancel context.CancelFunc
}

func NewPlaylistSource(path string, hub *Hub) *PlaylistSource {
	return &PlaylistSource{path: path, hub: hub}
}

func (p *PlaylistSource) CurrentTrack() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.track
}

func (p *PlaylistSource) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *PlaylistSource) Start() error {
	tracks, err := readPlaylist(p.path)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return fmt.Errorf("playlist %q is empty", p.path)
	}

	p.hub.SetFormat("audio/mpeg", nil)

	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.active = true
	p.mu.Unlock()

	go p.loop(ctx, tracks)
	return nil
}

func (p *PlaylistSource) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.active = false
	p.track = ""
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.hub.CloseAll()
}

func (p *PlaylistSource) loop(ctx context.Context, tracks []string) {
	for {
		for _, track := range tracks {
			select {
			case <-ctx.Done():
				return
			default:
			}

			name := trackName(track)
			p.mu.Lock()
			p.track = name
			p.mu.Unlock()
			log.Printf("streaming: %s", name)

			if err := p.streamFile(ctx, track); err != nil {
				log.Printf("error streaming %s: %v", name, err)
				// brief pause before trying next track
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
		}
	}
}

func (p *PlaylistSource) streamFile(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const chunkSize = 8192
	buf := make([]byte, chunkSize)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := f.Read(buf)
		if n > 0 {
			p.hub.Broadcast(buf[:n])
		}
		if err != nil {
			// EOF or read error – move on to next file
			return nil
		}

		// Throttle to approximately 320 kbps to act like a radio broadcast.
		// 8192 bytes * 8 bits / 320000 bps ≈ 205 ms per chunk.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func readPlaylist(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tracks []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tracks = append(tracks, line)
	}
	return tracks, scanner.Err()
}

func trackName(path string) string {
	// Return just the filename without the full path.
	if idx := strings.LastIndexByte(path, '/'); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
