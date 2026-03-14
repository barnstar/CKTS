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
    flex-direction: row;
    justify-content: center;
    gap: 16px;
    margin-bottom: 24px;
  }
  .vu-wrap.visible { display: flex; }
  .vu-meter {
    width: 200px;
    height: 140px;
    background: linear-gradient(180deg, #e8ddd0 0%, #d8ccb8 100%);
    border: 3px solid #888;
    border-radius: 12px;
    box-shadow: inset 0 2px 6px rgba(0,0,0,0.25), 0 4px 12px rgba(0,0,0,0.3);
    position: relative;
    overflow: hidden;
  }
  .vu-meter canvas {
    width: 100%;
    height: 100%;
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
    <div class="vu-meter"><canvas id="vuCanvasL"></canvas></div>
    <div class="vu-meter"><canvas id="vuCanvasR"></canvas></div>
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
const vuCanvasL = document.getElementById('vuCanvasL');
const vuCanvasR = document.getElementById('vuCanvasR');
const vuCtxL    = vuCanvasL.getContext('2d');
const vuCtxR    = vuCanvasR.getContext('2d');

let listening = false;
let audioCtx = null;
let analyserL = null;
let analyserR = null;
let vuAnimId = null;
let needleL = 0;
let needleR = 0;

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
  needleL = 0; needleR = 0;
  drawMeter(vuCtxL, vuCanvasL, 0, 'L');
  drawMeter(vuCtxR, vuCanvasR, 0, 'R');
}

function rms(analyser) {
  const buf = new Float32Array(analyser.fftSize);
  analyser.getFloatTimeDomainData(buf);
  let sum = 0;
  for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i];
  return Math.sqrt(sum / buf.length);
}

// Map dB (-40..+3) to needle fraction (0..1).
function dbToFrac(db) {
  // VU scale is non-linear. We use a piecewise mapping that mimics a real VU.
  // -20 is the 0 VU reference; above 0 VU is the red zone (+1..+3).
  const clamped = Math.max(-40, Math.min(3, db));
  // Map: -40 -> 0, -20 -> 0.56, 0 -> 0.88, +3 -> 1.0
  if (clamped <= -20) return 0.56 * (clamped + 40) / 20;
  if (clamped <= 0)   return 0.56 + 0.32 * (clamped + 20) / 20;
  return 0.88 + 0.12 * clamped / 3;
}

function drawVU() {
  vuAnimId = requestAnimationFrame(drawVU);
  if (!analyserL || !analyserR) return;

  const dbL = 20 * Math.log10(rms(analyserL) + 1e-10);
  const dbR = 20 * Math.log10(rms(analyserR) + 1e-10);
  const targetL = dbToFrac(dbL);
  const targetR = dbToFrac(dbR);

  // Smooth needle movement (fast attack, slow release like real VU).
  const attack = 0.3;
  const release = 0.08;
  needleL += (targetL > needleL ? attack : release) * (targetL - needleL);
  needleR += (targetR > needleR ? attack : release) * (targetR - needleR);

  drawMeter(vuCtxL, vuCanvasL, needleL, 'L');
  drawMeter(vuCtxR, vuCanvasR, needleR, 'R');
}

function drawMeter(ctx, canvas, frac, label) {
  const dpr = window.devicePixelRatio || 1;
  const w = canvas.clientWidth;
  const h = canvas.clientHeight;
  canvas.width = w * dpr;
  canvas.height = h * dpr;
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, w, h);

  const cx = w / 2;
  const cy = h * 0.92;
  const r = w * 0.42;

  // Arc angles: left to right sweep.
  const aStart = Math.PI + 0.35;  // ~215 deg
  const aEnd   = -0.35;           // ~-20 deg (i.e. ~340 deg)

  // ---- Scale markings ----
  // dB values and their positions on the arc.
  const marks = [
    {db: '-40', frac: 0},
    {db: '-30', frac: 0.28},
    {db: '-20', frac: 0.56},
    {db: '-10', frac: 0.72},
    {db: '-7',  frac: 0.78},
    {db: '-5',  frac: 0.82},
    {db: '-3',  frac: 0.855},
    {db: '0',   frac: 0.88},
    {db: '+1',  frac: 0.92},
    {db: '+2',  frac: 0.96},
    {db: '+3',  frac: 1.0},
  ];

  // Draw arc.
  ctx.strokeStyle = '#444';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.arc(cx, cy, r, aStart, aEnd);
  ctx.stroke();

  // Red zone arc (0 to +3).
  const redStart = aStart + (aEnd - aStart) * 0.88;
  ctx.strokeStyle = '#cc3333';
  ctx.lineWidth = 3;
  ctx.beginPath();
  ctx.arc(cx, cy, r, redStart, aEnd);
  ctx.stroke();

  // Tick marks and labels.
  ctx.lineWidth = 1;
  marks.forEach(m => {
    const a = aStart + (aEnd - aStart) * m.frac;
    const cos = Math.cos(a);
    const sin = Math.sin(a);
    const isRed = m.frac >= 0.88;
    const isMajor = ['0', '-10', '-20', '-30', '-40', '+3'].includes(m.db);
    const tickLen = isMajor ? 10 : 6;

    ctx.strokeStyle = isRed ? '#cc3333' : '#444';
    ctx.beginPath();
    ctx.moveTo(cx + cos * (r - tickLen), cy + sin * (r - tickLen));
    ctx.lineTo(cx + cos * r, cy + sin * r);
    ctx.stroke();

    if (isMajor || isRed) {
      ctx.fillStyle = isRed ? '#cc3333' : '#333';
      ctx.font = (isMajor ? 'bold ' : '') + '9px system-ui, sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(m.db, cx + cos * (r + 11), cy + sin * (r + 11));
    }
  });

  // "VU" label.
  ctx.fillStyle = '#555';
  ctx.font = 'bold 11px serif';
  ctx.textAlign = 'center';
  ctx.fillText('VU', cx, cy - r * 0.35);

  // Channel label.
  ctx.fillStyle = '#888';
  ctx.font = '9px system-ui, sans-serif';
  ctx.fillText(label, cx, cy - r * 0.18);

  // ---- Needle ----
  const needleAngle = aStart + (aEnd - aStart) * Math.max(0, Math.min(1, frac));
  const needleLen = r - 14;

  // Shadow.
  ctx.save();
  ctx.shadowColor = 'rgba(0,0,0,0.3)';
  ctx.shadowBlur = 4;
  ctx.shadowOffsetY = 2;
  ctx.strokeStyle = '#222';
  ctx.lineWidth = 2;
  ctx.lineCap = 'round';
  ctx.beginPath();
  ctx.moveTo(cx, cy);
  ctx.lineTo(cx + Math.cos(needleAngle) * needleLen, cy + Math.sin(needleAngle) * needleLen);
  ctx.stroke();
  ctx.restore();

  // Pivot dot.
  ctx.fillStyle = '#333';
  ctx.beginPath();
  ctx.arc(cx, cy, 4, 0, Math.PI * 2);
  ctx.fill();
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
