package main

import (
	"net"
	"sync"
)

// Client represents a single connected listener.
type Client struct {
	ch         chan []byte
	remoteAddr string
	closeOnce  sync.Once
}

func (c *Client) close() {
	c.closeOnce.Do(func() { close(c.ch) })
}

// Hub broadcasts audio chunks to all connected clients.
type Hub struct {
	mu          sync.Mutex
	clients     map[*Client]struct{}
	contentType string
	header      []byte // sent to every new client (e.g. WAV header)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]struct{})}
}

// SetFormat updates the content-type and per-client header.
// Call this before starting the audio source.
func (h *Hub) SetFormat(contentType string, header []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.contentType = contentType
	h.header = header
}

func (h *Hub) ContentType() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.contentType
}

// Subscribe registers a new client and returns it.
// remoteAddr is the client's address (used for unique listener counting).
// The header (if any) is queued immediately.
func (h *Hub) Subscribe(remoteAddr string) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &Client{ch: make(chan []byte, 128), remoteAddr: remoteAddr}
	h.clients[c] = struct{}{}
	if len(h.header) > 0 {
		buf := make([]byte, len(h.header))
		copy(buf, h.header)
		c.ch <- buf
	}
	return c
}

// Unsubscribe removes the client and closes its channel.
func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	c.close()
}

// Broadcast sends data to every connected client.
// Slow clients get their chunk dropped rather than blocking.
func (h *Hub) Broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		buf := make([]byte, len(data))
		copy(buf, data)
		select {
		case c.ch <- buf:
		default:
			// slow client – drop this chunk
		}
	}
}

// CloseAll disconnects every client (e.g. on Stop).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.close()
		delete(h.clients, c)
	}
}

// ClientCount returns the number of currently connected streams.
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// ListenerCount returns the number of unique listeners (by IP, ignoring port).
// Browsers often open multiple connections per tab, so this avoids double-counting.
func (h *Hub) ListenerCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[string]struct{})
	for c := range h.clients {
		host, _, err := net.SplitHostPort(c.remoteAddr)
		if err != nil {
			host = c.remoteAddr
		}
		seen[host] = struct{}{}
	}
	return len(seen)
}
