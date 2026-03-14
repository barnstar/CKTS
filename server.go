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
    justify-content: center;
    margin-bottom: 24px;
  }
  .vu-wrap.visible { display: flex; }
  .vu-meter {
    width: 320px;
    height: 200px;
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
  audio { display: none; }
  .vol-wrap {
    display: none;
    align-items: center;
    gap: 10px;
    margin-bottom: 20px;
    padding: 10px 16px;
    background: linear-gradient(180deg, #e8ddd0 0%, #d8ccb8 100%);
    border: 3px solid #888;
    border-radius: 10px;
    box-shadow: inset 0 2px 6px rgba(0,0,0,0.15), 0 2px 8px rgba(0,0,0,0.2);
  }
  .vol-wrap.visible { display: flex; }
  .vol-wrap .vol-icon {
    font-size: 1.1rem;
    color: #555;
    cursor: pointer;
    user-select: none;
    min-width: 22px;
    text-align: center;
  }
  .vol-wrap input[type=range] {
    flex: 1;
    -webkit-appearance: none;
    appearance: none;
    height: 6px;
    border-radius: 3px;
    background: linear-gradient(90deg, #bbb 0%, #888 100%);
    outline: none;
    cursor: pointer;
  }
  .vol-wrap input[type=range]::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: radial-gradient(circle at 40% 35%, #eee, #999);
    border: 2px solid #666;
    box-shadow: 0 1px 4px rgba(0,0,0,0.3);
  }
  .vol-wrap input[type=range]::-moz-range-thumb {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: radial-gradient(circle at 40% 35%, #eee, #999);
    border: 2px solid #666;
    box-shadow: 0 1px 4px rgba(0,0,0,0.3);
  }
  .vol-wrap .vol-pct {
    font-size: 0.75rem;
    color: #555;
    min-width: 32px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
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
    <div class="vu-meter"><canvas id="vuCanvas"></canvas></div>
  </div>

  <audio id="player"></audio>

  <div class="vol-wrap" id="volWrap">
    <span class="vol-icon" id="volIcon" onclick="toggleMute()">&#128266;</span>
    <input type="range" id="volSlider" min="0" max="100" value="80">
    <span class="vol-pct" id="volPct">80%</span>
  </div>

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
const vuWrap   = document.getElementById('vuWrap');
const vuCanvas = document.getElementById('vuCanvas');
const vuCtx    = vuCanvas.getContext('2d');
const volWrap  = document.getElementById('volWrap');
const volSlider = document.getElementById('volSlider');
const volIcon  = document.getElementById('volIcon');
const volPct   = document.getElementById('volPct');

let listening = false;
let audioCtx = null;
let analyserL = null;
let analyserR = null;
let vuAnimId = null;
let needlePos = 0;
let savedVol = 80;

// Volume slider wiring.
player.volume = 0.8;
volSlider.addEventListener('input', () => {
  const v = parseInt(volSlider.value, 10);
  player.volume = v / 100;
  volPct.textContent = v + '%';
  volIcon.innerHTML = v === 0 ? '&#128264;' : v < 50 ? '&#128265;' : '&#128266;';
  if (v > 0) savedVol = v;
});

function toggleMute() {
  if (player.volume > 0) {
    savedVol = parseInt(volSlider.value, 10);
    volSlider.value = 0;
    player.volume = 0;
    volPct.textContent = '0%';
    volIcon.innerHTML = '&#128264;';
  } else {
    volSlider.value = savedVol;
    player.volume = savedVol / 100;
    volPct.textContent = savedVol + '%';
    volIcon.innerHTML = savedVol < 50 ? '&#128265;' : '&#128266;';
  }
}

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
    vuWrap.classList.add('visible');
    volWrap.classList.add('visible');
  } else {
    toggleBtn.innerHTML = '&#9654; Listen';
    toggleBtn.classList.remove('on');
    vuWrap.classList.remove('visible');
    volWrap.classList.remove('visible');
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
  needlePos = 0;
  drawMeter(0);
}

function rms(analyser) {
  const buf = new Float32Array(analyser.fftSize);
  analyser.getFloatTimeDomainData(buf);
  let sum = 0;
  for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i];
  return Math.sqrt(sum / buf.length);
}

// Map dB (-40..+3) to fraction (0..1) across the arc.
function dbToFrac(db) {
  const clamped = Math.max(-40, Math.min(3, db));
  if (clamped <= -20) return 0.56 * (clamped + 40) / 20;
  if (clamped <= 0)   return 0.56 + 0.32 * (clamped + 20) / 20;
  return 0.88 + 0.12 * clamped / 3;
}

function drawVU() {
  vuAnimId = requestAnimationFrame(drawVU);
  if (!analyserL || !analyserR) return;

  // Combine L+R into mono RMS.
  const monoRms = (rms(analyserL) + rms(analyserR)) / 2;
  const db = 20 * Math.log10(monoRms + 1e-10);
  const target = dbToFrac(db);

  // Smooth needle: fast attack, slow release.
  const attack = 0.3;
  const release = 0.08;
  needlePos += (target > needlePos ? attack : release) * (target - needlePos);

  drawMeter(needlePos);
}

function drawMeter(frac) {
  const dpr = window.devicePixelRatio || 1;
  const w = vuCanvas.clientWidth;
  const h = vuCanvas.clientHeight;
  vuCanvas.width = w * dpr;
  vuCanvas.height = h * dpr;
  vuCtx.scale(dpr, dpr);
  vuCtx.clearRect(0, 0, w, h);

  const cx = w / 2;
  const cy = h * 0.88;            // pivot near bottom
  const r  = Math.min(w, h) * 0.6; // needle length / arc radius

  // Sweep: -60° to +60° where 0° = straight up.
  // In canvas coords, straight up = -π/2.
  const DEG = Math.PI / 180;
  const aStart = -90 * DEG - 60 * DEG;  // -150° = left side
  const aEnd   = -90 * DEG + 60 * DEG;  // -30°  = right side

  // ---- Scale markings ----
  const marks = [
    {db: '-40', frac: 0.00},
    {db: '-30', frac: 0.28},
    {db: '-20', frac: 0.56},
    {db: '-10', frac: 0.72},
    {db: '-7',  frac: 0.78},
    {db: '-5',  frac: 0.82},
    {db: '-3',  frac: 0.855},
    {db: '0',   frac: 0.88},
    {db: '+1',  frac: 0.92},
    {db: '+2',  frac: 0.96},
    {db: '+3',  frac: 1.00},
  ];

  // Main arc.
  vuCtx.strokeStyle = '#444';
  vuCtx.lineWidth = 1.5;
  vuCtx.beginPath();
  vuCtx.arc(cx, cy, r, aStart, aEnd);
  vuCtx.stroke();

  // Red zone arc (0 dB to +3 dB).
  const redAngle = aStart + (aEnd - aStart) * 0.88;
  vuCtx.strokeStyle = '#cc3333';
  vuCtx.lineWidth = 3;
  vuCtx.beginPath();
  vuCtx.arc(cx, cy, r, redAngle, aEnd);
  vuCtx.stroke();

  // Tick marks and dB labels.
  vuCtx.lineWidth = 1;
  marks.forEach(m => {
    const a = aStart + (aEnd - aStart) * m.frac;
    const cos = Math.cos(a);
    const sin = Math.sin(a);
    const isRed = m.frac >= 0.88;
    const isMajor = ['-40','-30','-20','-10','0','+3'].includes(m.db);
    const tickLen = isMajor ? 12 : 7;

    vuCtx.strokeStyle = isRed ? '#cc3333' : '#444';
    vuCtx.beginPath();
    vuCtx.moveTo(cx + cos * (r - tickLen), cy + sin * (r - tickLen));
    vuCtx.lineTo(cx + cos * r, cy + sin * r);
    vuCtx.stroke();

    if (isMajor || isRed) {
      vuCtx.fillStyle = isRed ? '#cc3333' : '#333';
      vuCtx.font = (isMajor ? 'bold ' : '') + '11px system-ui, sans-serif';
      vuCtx.textAlign = 'center';
      vuCtx.textBaseline = 'middle';
      vuCtx.fillText(m.db, cx + cos * (r + 14), cy + sin * (r + 14));
    }
  });

  // "VU" label.
  vuCtx.fillStyle = '#555';
  vuCtx.font = 'bold 14px serif';
  vuCtx.textAlign = 'center';
  vuCtx.fillText('VU', cx, cy - r * 0.32);

  // ---- Needle ----
  const needleAngle = aStart + (aEnd - aStart) * Math.max(0, Math.min(1, frac));
  const needleLen = r - 16;

  // Shadow.
  vuCtx.save();
  vuCtx.shadowColor = 'rgba(0,0,0,0.25)';
  vuCtx.shadowBlur = 4;
  vuCtx.shadowOffsetY = 2;
  vuCtx.strokeStyle = '#222';
  vuCtx.lineWidth = 2;
  vuCtx.lineCap = 'round';
  vuCtx.beginPath();
  vuCtx.moveTo(cx, cy);
  vuCtx.lineTo(cx + Math.cos(needleAngle) * needleLen, cy + Math.sin(needleAngle) * needleLen);
  vuCtx.stroke();
  vuCtx.restore();

  // Pivot dot.
  vuCtx.fillStyle = '#333';
  vuCtx.beginPath();
  vuCtx.arc(cx, cy, 5, 0, Math.PI * 2);
  vuCtx.fill();
}

// Sync custom button with audio element events.
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
