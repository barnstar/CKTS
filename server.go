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
    font-family: 'Impact', 'Arial Black', 'Helvetica Neue', sans-serif;
    font-size: 3rem;
    font-weight: 900;
    font-style: italic;
    color: #ffffff;
    text-transform: uppercase;
    text-align: center;
    letter-spacing: 0.04em;
    text-shadow: 3px 3px 0 #111, 5px 5px 10px rgba(0,0,0,0.6);
    margin-bottom: 32px;
  }
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
  .vu-wrap {
    display: none;
    margin-bottom: 24px;
  }
  .vu-wrap.visible { display: block; }
  .vu-canvas {
    width: 100%;
    height: 48px;
    border-radius: 8px;
    background: #111;
  }
  .vu-labels {
    display: flex;
    justify-content: space-between;
    font-size: 0.65rem;
    color: #505070;
    margin-top: 4px;
    padding: 0 2px;
  }
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

  <div class="vu-wrap" id="vuWrap">
    <canvas class="vu-canvas" id="vuCanvas"></canvas>
    <div class="vu-labels">
      <span>-40</span><span>-30</span><span>-20</span><span>-10</span><span>-5</span><span>0 dB</span>
    </div>
  </div>

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
const vuWrap    = document.getElementById('vuWrap');
const vuCanvas  = document.getElementById('vuCanvas');
const vuCtx     = vuCanvas.getContext('2d');

let listening = false;
let audioCtx = null;
let analyserL = null;
let analyserR = null;
let vuAnimId = null;

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
    vuWrap.classList.add('visible');
  } else {
    toggleBtn.innerHTML = '&#9654; Listen';
    toggleBtn.classList.remove('on');
    player.classList.remove('visible');
    vuWrap.classList.remove('visible');
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
  initVU();
}

function stopListening() {
  player.pause();
  player.src = '';
  listening = false;
  updateToggle();
  stopVU();
}

// ---- VU Meter (Web Audio API) ----

function initVU() {
  if (!audioCtx) {
    audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  }
  if (audioCtx.state === 'suspended') audioCtx.resume();

  const src = audioCtx.createMediaElementSource(player);
  const splitter = audioCtx.createChannelSplitter(2);

  analyserL = audioCtx.createAnalyser();
  analyserR = audioCtx.createAnalyser();
  analyserL.fftSize = 1024;
  analyserR.fftSize = 1024;
  analyserL.smoothingTimeConstant = 0.8;
  analyserR.smoothingTimeConstant = 0.8;

  src.connect(splitter);
  splitter.connect(analyserL, 0);
  splitter.connect(analyserR, 1);
  src.connect(audioCtx.destination);

  drawVU();
}

function stopVU() {
  if (vuAnimId) {
    cancelAnimationFrame(vuAnimId);
    vuAnimId = null;
  }
  // Clear the canvas.
  vuCanvas.width = vuCanvas.clientWidth * (window.devicePixelRatio || 1);
  vuCanvas.height = vuCanvas.clientHeight * (window.devicePixelRatio || 1);
  vuCtx.clearRect(0, 0, vuCanvas.width, vuCanvas.height);
}

function rms(analyser) {
  const buf = new Float32Array(analyser.fftSize);
  analyser.getFloatTimeDomainData(buf);
  let sum = 0;
  for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i];
  return Math.sqrt(sum / buf.length);
}

function drawVU() {
  vuAnimId = requestAnimationFrame(drawVU);
  if (!analyserL || !analyserR) return;

  const dpr = window.devicePixelRatio || 1;
  const w = vuCanvas.clientWidth;
  const h = vuCanvas.clientHeight;
  vuCanvas.width = w * dpr;
  vuCanvas.height = h * dpr;
  vuCtx.scale(dpr, dpr);

  vuCtx.clearRect(0, 0, w, h);

  const barH = 16;
  const gap = 6;
  const yL = (h - 2 * barH - gap) / 2;
  const yR = yL + barH + gap;
  const pad = 4;
  const maxW = w - pad * 2;

  // RMS to dB, clamp -40..0
  const dbL = Math.max(-40, Math.min(0, 20 * Math.log10(rms(analyserL) + 1e-10)));
  const dbR = Math.max(-40, Math.min(0, 20 * Math.log10(rms(analyserR) + 1e-10)));
  const fracL = (dbL + 40) / 40;
  const fracR = (dbR + 40) / 40;

  drawBar(yL, fracL, maxW, barH, pad);
  drawBar(yR, fracR, maxW, barH, pad);

  // Channel labels.
  vuCtx.fillStyle = '#505070';
  vuCtx.font = '10px system-ui, sans-serif';
  vuCtx.textBaseline = 'middle';
}

function drawBar(y, frac, maxW, barH, pad) {
  // Background track.
  vuCtx.fillStyle = '#1a1a2a';
  vuCtx.beginPath();
  vuCtx.roundRect(pad, y, maxW, barH, 4);
  vuCtx.fill();

  const bw = frac * maxW;
  if (bw < 1) return;

  // Gradient: teal -> yellow -> red.
  const grad = vuCtx.createLinearGradient(pad, 0, pad + maxW, 0);
  grad.addColorStop(0,    '#2a8a7a');
  grad.addColorStop(0.6,  '#4a9a6a');
  grad.addColorStop(0.8,  '#b0a040');
  grad.addColorStop(1,    '#c04040');
  vuCtx.fillStyle = grad;
  vuCtx.beginPath();
  vuCtx.roundRect(pad, y, bw, barH, 4);
  vuCtx.fill();
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
