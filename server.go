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
    justify-content: center;
    margin-bottom: 20px;
  }
  .on-air-sign {
    display: inline-block;
    padding: 10px 32px;
    border-radius: 8px;
    border: 4px solid #333;
    background: #2a1a1a;
    color: #553333;
    font-family: 'Arial Black', 'Helvetica Neue', Arial, sans-serif;
    font-size: 1.6rem;
    font-weight: 900;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    text-align: center;
    user-select: none;
    transition: all 0.4s ease;
    box-shadow: inset 0 2px 8px rgba(0,0,0,0.5);
  }
  .on-air-sign.live {
    background: #cc2222;
    color: #fff;
    border-color: #444;
    box-shadow: inset 0 2px 8px rgba(0,0,0,0.3), 0 0 20px rgba(204,34,34,0.5), 0 0 60px rgba(204,34,34,0.2);
    text-shadow: 0 0 10px rgba(255,255,255,0.5);
  }
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
  #toggleBtn { background: #4a7a8a; color: #fff; }
  #toggleBtn.on { background: #6a7a88; }
  .error { color: #d09090; font-size: 0.85rem; margin-top: 16px; min-height: 1.2em; }
</style>
</head>
<body>
<div class="card">
  <h1>{{CALLSIGN}} Radio</h1>

  <div class="status-row">
    <div class="on-air-sign live" id="onAirSign">ON AIR</div>
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
const player    = document.getElementById('player');
const onAirSign = document.getElementById('onAirSign');
const trackEl   = document.getElementById('track');
const clientEl  = document.getElementById('clients');
const errMsg    = document.getElementById('errMsg');
const toggleBtn = document.getElementById('toggleBtn');

let listening = false;

function setError(msg) { errMsg.textContent = msg; }

function applyStatus(data) {
  setError('');
  if (data.playing) {
    onAirSign.classList.add('live');
    onAirSign.textContent = 'ON AIR';
    trackEl.textContent = data.track || '—';
    clientEl.textContent = data.clients + ' listener' + (data.clients === 1 ? '' : 's');
  } else {
    onAirSign.classList.remove('live');
    onAirSign.textContent = 'OFF AIR';
    trackEl.textContent = '—';
    clientEl.textContent = '';
  }
}

function updateToggle() {
  if (listening) {
    toggleBtn.innerHTML = '&#9632; Stop Listening';
    toggleBtn.classList.add('on');
    player.classList.add('visible');
  } else {
    toggleBtn.innerHTML = '&#9654; Listen';
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
  if (listening) {
    stopListening();
  } else {
    startListening();
  }
}

function startListening() {
  listening = true;
  updateToggle();
  player.src = '/stream?' + Date.now();
  player.play().catch(() => {});
}

function stopListening() {
  player.pause();
  player.src = '';
  listening = false;
  updateToggle();
}

// Sync custom button with native audio controls.
player.addEventListener('pause', () => {
  if (listening) {
    listening = false;
    updateToggle();
  }
});
player.addEventListener('play', () => {
  if (!listening) {
    listening = true;
    updateToggle();
  }
});

setInterval(pollStatus, 3000);
pollStatus();
</script>
</body>
</html>
`
