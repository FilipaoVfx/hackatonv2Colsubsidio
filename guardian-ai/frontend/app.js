// Mission Control — read-only consumer of the backend event stream (RN-004).
const $ = (id) => document.getElementById(id);
const STAGES = ["Discovery", "Profile", "Risk", "Recommendation", "Objection", "Acceptance", "Summary"];

let totalTokens = 0, totalCost = 0;

function initPipeline() {
  const p = $("pipeline");
  p.innerHTML = "";
  STAGES.forEach((s) => {
    const el = document.createElement("span");
    el.className = "stage";
    el.dataset.stage = s;
    el.textContent = s;
    p.appendChild(el);
  });
}
function markStage(name, cls) {
  document.querySelectorAll(".stage").forEach((el) => {
    if (el.dataset.stage === name) el.classList.add(cls);
  });
}

function addTimeline(ev) {
  const li = document.createElement("li");
  const meta = ev.payload && (ev.payload.text || ev.payload.to || ev.payload.tool ||
    ev.payload.key || ev.payload.intent || ev.payload.product_name || ev.payload.reason || "");
  li.innerHTML = `<div class="t-type">${ev.type}</div>
    <div class="t-meta">#${ev.sequence} · ${ev.producer}${meta ? " · " + String(meta).slice(0, 40) : ""}</div>`;
  const tl = $("timeline");
  tl.prepend(li);
  while (tl.children.length > 120) tl.removeChild(tl.lastChild);
}

function addMsg(role, text) {
  const d = document.createElement("div");
  d.className = "msg " + (role === "user" ? "user" : "agent");
  d.innerHTML = `<div class="who">${role}</div>${text}`;
  const t = $("transcript");
  t.appendChild(d);
  t.scrollTop = t.scrollHeight;
}

const features = {};
function renderFeatures() {
  const box = $("features");
  box.innerHTML = "";
  Object.entries(features).forEach(([k, v]) => {
    const el = document.createElement("div");
    el.className = "feat";
    el.innerHTML = `<span class="fk">${k}</span><span>${v}</span>`;
    box.appendChild(el);
  });
}

function handle(ev) {
  addTimeline(ev);
  const p = ev.payload || {};
  switch (ev.type) {
    case "CALL_STARTED":
      resetView();
      markStage("Discovery", "active");
      break;
    case "STATE_CHANGED":
      $("stateBadge").textContent = "state: " + p.to;
      $("stateBadge").className = "pill pill-on";
      break;
    case "TRANSCRIPT_UPDATED":
      if (p.is_final) {
        removeThinking();
        addMsg(p.role, p.text);
        if (p.role === "agent") speak(p.text);
      }
      break;
    case "FEATURE_UPDATED":
      if (p.key === "risk_level") {
        $("mRisk").textContent = p.value;
        $("mRisk").className = "v risk-" + p.value;
        markStage("Risk", "done");
      } else if (p.key === "sentiment") {
        $("mSent").textContent = p.value;
      } else {
        features[p.key] = p.value;
        renderFeatures();
        markStage("Profile", "done");
      }
      break;
    case "INTENT_DETECTED":
      $("mIntent").textContent = p.intent + " (" + Math.round(p.confidence * 100) + "%)";
      if (p.intent === "price_objection") markStage("Objection", "done");
      if (p.intent === "acceptance") markStage("Acceptance", "done");
      break;
    case "LLM_REQUESTED":
      if (p.strategy) $("mNarr").textContent = p.strategy;
      break;
    case "LLM_RESPONSE":
      if (p.strategy) $("mNarr").textContent = p.strategy;
      totalTokens += (p.tokens_in || 0) + (p.tokens_out || 0);
      totalCost += p.cost_usd || 0;
      $("mLatency").textContent = p.latency_ms || 0;
      $("mTokens").textContent = totalTokens;
      $("mCost").textContent = "$" + totalCost.toFixed(4);
      break;
    case "TOOL_CALLED":
    case "TOOL_EXECUTED": {
      const box = $("tools");
      if (box.classList.contains("muted")) { box.classList.remove("muted"); box.innerHTML = ""; }
      const row = document.createElement("div");
      row.className = "tool-row";
      row.textContent = `${ev.type === "TOOL_CALLED" ? "→" : "✓"} ${p.tool}` +
        (p.latency_ms ? ` · ${p.latency_ms}ms` : "");
      box.appendChild(row);
      break;
    }
    case "RECOMMENDATION_GENERATED": {
      const r = $("reco");
      r.className = "reco on";
      r.innerHTML = `<div class="rp">${p.product_name}</div>
        <div class="rr">${p.reasoning}</div>
        <div class="rr">Factores: ${(p.profile_factors || []).join(", ")} · conf. ${Math.round(p.confidence*100)}%</div>`;
      markStage("Recommendation", "done");
      break;
    }
    case "SUMMARY_GENERATED":
      markStage("Summary", "done");
      addMsg("agent", "<em>Resumen:</em> " + p.summary);
      break;
    case "CALL_ENDED":
      $("stateBadge").textContent = "state: ENDED";
      break;
    case "ERROR_OCCURRED":
      addMsg("agent", "<em>Error:</em> " + (p.message || p.code));
      break;
  }
}

function resetView() {
  totalTokens = 0; totalCost = 0;
  for (const k in features) delete features[k];
  $("transcript").innerHTML = "";
  $("features").innerHTML = "";
  $("reco").className = "reco muted";
  $("reco").textContent = "Sin recomendación aún.";
  $("tools").className = "tools muted";
  $("tools").textContent = "—";
  ["mNarr", "mRisk", "mSent", "mIntent"].forEach((id) => { $(id).textContent = "—"; $(id).className = "v"; });
  $("mLatency").textContent = "0"; $("mTokens").textContent = "0"; $("mCost").textContent = "$0.0000";
  initPipeline();
}

function connect() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => { $("wsStatus").textContent = "WS: conectado"; $("wsStatus").className = "pill pill-on"; };
  ws.onclose = () => {
    $("wsStatus").textContent = "WS: reconectando…"; $("wsStatus").className = "pill pill-off";
    setTimeout(connect, 1500);
  };
  ws.onmessage = (m) => { try { handle(JSON.parse(m.data)); } catch (e) { console.error(e); } };
}

// ---- Real conversation (GPT-4o) ----
let callID = null;

function showThinking() {
  removeThinking();
  const d = document.createElement("div");
  d.className = "thinking"; d.id = "thinkingRow";
  d.textContent = "Guardian AI está pensando…";
  $("transcript").appendChild(d);
  $("transcript").scrollTop = $("transcript").scrollHeight;
}
function removeThinking() { const t = $("thinkingRow"); if (t) t.remove(); }

async function startConversation() {
  const r = await fetch("/api/calls/start", { method: "POST" });
  if (!r.ok) { alert("Motor LLM no configurado (falta OPENAI_API_KEY)"); return; }
  callID = (await r.json()).call_id;
  $("msgInput").focus();
}

async function sendTurn() {
  const text = $("msgInput").value.trim();
  if (!text) return;
  if (!callID) { await startConversation(); if (!callID) return; }
  $("msgInput").value = "";
  showThinking();
  const r = await fetch(`/api/calls/${callID}/turn`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  });
  if (!r.ok) { removeThinking(); addMsg("agent", "<em>Error:</em> " + (await r.text())); }
}

$("startBtn").addEventListener("click", startConversation);
$("sendBtn").addEventListener("click", sendTurn);
$("msgInput").addEventListener("keydown", (e) => { if (e.key === "Enter") sendTurn(); });

// ---- Voice out: browser Web Speech (free stand-in for ElevenLabs) ----
function speak(text) {
  if (!$("voiceToggle").checked || !window.speechSynthesis || !text) return;
  const u = new SpeechSynthesisUtterance(text);
  u.lang = "es-ES"; u.rate = 1.05;
  window.speechSynthesis.cancel();
  window.speechSynthesis.speak(u);
}

// ---- Voice in: browser SpeechRecognition ----
const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
let rec = null;
if (SR) {
  rec = new SR(); rec.lang = "es-ES"; rec.interimResults = false;
  rec.onresult = (e) => { $("msgInput").value = e.results[0][0].transcript; sendTurn(); };
  rec.onend = () => $("micBtn").classList.remove("rec");
  $("micBtn").addEventListener("click", () => {
    $("micBtn").classList.add("rec");
    try { rec.start(); } catch (_) {}
  });
} else {
  $("micBtn").style.display = "none";
}

// ---- Mock simulate (offline, no API cost) ----
$("simBtn").addEventListener("click", async () => {
  $("simBtn").disabled = true;
  callID = null;
  try { await fetch("/api/calls/simulate", { method: "POST" }); }
  finally { setTimeout(() => ($("simBtn").disabled = false), 6000); }
});

initPipeline();
connect();
