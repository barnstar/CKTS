package main

import (
	"encoding/json"
	"html"
	"log"
	"net/http"
	"strings"
)

// Server holds the HTTP mux and references to the hub and audio source.
type Server struct {
	hub      *Hub
	src      AudioSource
	mux      *http.ServeMux
	callsign string
}

func NewServer(hub *Hub, src AudioSource, callsign string) *Server {
	s := &Server{hub: hub, src: src, mux: http.NewServeMux(), callsign: callsign}
	s.mux.HandleFunc("/", s.handleUI)
	s.mux.HandleFunc("/stream", s.handleStream)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/start", s.handleStart)
	s.mux.HandleFunc("/api/stop", s.handleStop)
	return s
}

func (s *Server) Router() http.Handler { return s.mux }

// handleUI serves the single-page web interface.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	pageHTML := strings.ReplaceAll(uiHTML, "{{CALLSIGN}}", html.EscapeString(s.callsign))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(pageHTML))
}

// handleStream is the audio streaming endpoint.
// Each connecting client receives audio chunks via chunked transfer encoding.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.src.IsPlaying() {
		http.Error(w, "stream not started", http.StatusServiceUnavailable)
		return
	}

	ct := s.hub.ContentType()
	if ct == "" {
		http.Error(w, "no stream available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	client := s.hub.Subscribe(r.RemoteAddr)
	defer s.hub.Unsubscribe(client)

	log.Printf("stream: client connected (%s)", r.RemoteAddr)
	defer log.Printf("stream: client disconnected (%s)", r.RemoteAddr)

	for {
		select {
		case chunk, ok := <-client.ch:
			if !ok {
				return // hub closed our channel (Stop was called)
			}
			if _, err := w.Write(chunk); err != nil {
				return // client disconnected
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return // client disconnected
		}
	}
}

type statusResponse struct {
	Playing bool   `json:"playing"`
	Track   string `json:"track"`
	Clients int    `json:"clients"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := statusResponse{
		Playing: s.src.IsPlaying(),
		Track:   s.src.CurrentTrack(),
		Clients: s.hub.ListenerCount(),
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.src.IsPlaying() {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := s.src.Start(); err != nil {
		log.Printf("start error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("stream started")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.src.Stop()
	log.Println("stream stopped")
	w.WriteHeader(http.StatusOK)
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{CALLSIGN}} Radio</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: system-ui, -apple-system, sans-serif;
    background: #0f0f1a;
    color: #e0e0f0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    padding: 20px;
  }
  .card {
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 16px;
    padding: 40px;
    max-width: 520px;
    width: 100%;
    box-shadow: 0 8px 32px rgba(0,0,0,0.4);
  }
  h1 {
    font-size: 2rem;
    color: #7c85ff;
    margin-bottom: 8px;
    letter-spacing: 0.05em;
  }
  h1 { margin-bottom: 32px; }
  .status-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 12px;
  }
  .dot {
    width: 10px; height: 10px;
    border-radius: 50%;
    background: #444;
    flex-shrink: 0;
    transition: background 0.3s;
  }
  .dot.live { background: #4caf50; box-shadow: 0 0 8px #4caf5080; animation: pulse 1.5s infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.5} }
  .status-label { font-size: 0.85rem; color: #8080b0; text-transform: uppercase; letter-spacing: 0.08em; }
  .track {
    font-size: 1.1rem;
    color: #a0a8ff;
    font-style: italic;
    min-height: 1.5em;
    margin-bottom: 8px;
    word-break: break-all;
  }
  .clients { font-size: 0.8rem; color: #505080; margin-bottom: 28px; }
  audio {
    width: 100%;
    margin-bottom: 24px;
    border-radius: 8px;
    accent-color: #7c85ff;
    display: none;
  }
  audio.visible { display: block; }
  .controls { display: flex; gap: 12px; }
  button {
    flex: 1;
    padding: 14px;
    font-size: 1rem;
    font-weight: 600;
    border: none;
    border-radius: 10px;
    cursor: pointer;
    transition: opacity 0.2s, transform 0.1s;
  }
  button:active { transform: scale(0.97); }
  button:disabled { opacity: 0.35; cursor: default; }
  #toggleBtn { background: #4caf50; color: #fff; }
  #toggleBtn.on { background: #e05050; }
  .error { color: #ff6060; font-size: 0.85rem; margin-top: 16px; min-height: 1.2em; }
</style>
</head>
<body>
<div class="card">
  <h1>{{CALLSIGN}} Radio</h1>

  <div class="status-row">
    <div class="dot" id="dot"></div>
    <span class="status-label" id="statusLabel">Stopped</span>
  </div>
  <div class="track" id="track">—</div>
  <div class="clients" id="clients"></div>

  <audio id="player" controls></audio>

  <div class="controls">
    <button id="toggleBtn" onclick="toggleStream()">&#9654; Listen</button>
  </div>
  <div class="error" id="errMsg"></div>
</div>

<script>
const player   = document.getElementById('player');
const dot      = document.getElementById('dot');
const label    = document.getElementById('statusLabel');
const trackEl  = document.getElementById('track');
const clientEl = document.getElementById('clients');
const errMsg   = document.getElementById('errMsg');
const toggleBtn = document.getElementById('toggleBtn');

let streaming = false;

function setError(msg) { errMsg.textContent = msg; }

function applyStatus(data) {
  setError('');
  if (data.playing) {
    dot.classList.add('live');
    label.textContent = 'Live';
    trackEl.textContent = data.track || '—';
    clientEl.textContent = data.clients + ' listener' + (data.clients === 1 ? '' : 's');
  } else {
    dot.classList.remove('live');
    label.textContent = 'Stopped';
    trackEl.textContent = '—';
    clientEl.textContent = '';
  }
  streaming = data.playing;
  updateToggle();
}

function updateToggle() {
  if (streaming) {
    toggleBtn.innerHTML = '&#9632; Stop';
    toggleBtn.classList.add('on');
    player.classList.add('visible');
  } else {
    toggleBtn.innerHTML = '&#9654; Start';
    toggleBtn.classList.remove('on');
    player.classList.remove('visible');
  }
}

function pollStatus() {
  fetch('/api/status')
    .then(r => r.json())
    .then(applyStatus)
    .catch(() => {});
}

function toggleStream() {
  if (streaming) {
    stopStream();
  } else {
    startStream();
  }
}

function startStream() {
  toggleBtn.disabled = true;
  fetch('/api/start', {method: 'POST'})
    .then(r => {
      if (!r.ok) return r.text().then(t => { throw new Error(t); });
      streaming = true;
      updateToggle();
      player.src = '/stream?' + Date.now();
      player.play().catch(() => {});
      pollStatus();
    })
    .catch(e => { setError('Start failed: ' + e.message); })
    .finally(() => { toggleBtn.disabled = false; });
}

function stopStream() {
  toggleBtn.disabled = true;
  player.pause();
  player.src = '';
  streaming = false;
  updateToggle();
  fetch('/api/stop', {method: 'POST'})
    .then(() => pollStatus())
    .catch(() => {})
    .finally(() => { toggleBtn.disabled = false; });
}

setInterval(pollStatus, 3000);
pollStatus();
</script>
</body>
</html>
`
