package main

// AudioSource is the interface implemented by all audio sources.
type AudioSource interface {
	// Start begins streaming audio into the hub.
	Start() error
	// Stop halts streaming and closes all connected clients.
	Stop()
	// CurrentTrack returns a human-readable description of what is playing.
	CurrentTrack() string
	// IsPlaying reports whether the source is currently active.
	IsPlaying() bool
}
