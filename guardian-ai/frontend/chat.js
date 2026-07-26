// Chat WhatsApp — hilo en vivo + panel de eventos en tiempo real + dark mode.
// Versión 4: realtime robusto.
//   - Re-attach tras reload: lee /api/whatsapp/sessions y reconstruye el hilo
//     y el feed desde /api/calls/:id/events (replay del EventStore, RN-003).
//   - Auto-attach: si llega un CALL_STARTED de canal whatsapp (p.ej. mensaje
//     real del celular vía Kapso) sin conversación activa en la UI, se engancha.
//   - Teléfono bloqueado durante la conversación (no se puede cambiar el destino).
//   - Debug: versión en consola + badge ?debug=1 con ids.
const CHAT_UI_VERSION = "4.0.0";
console.log(`[guardian-ai] chat UI v${CHAT_UI_VERSION}`);
const DEBUG = new URLSearchParams(location.search).has("debug");

const $ = (id) => document.getElementById(id);

let convID = null;
let phone = "";
let typingEl = null;

const now = () => new Date().toLocaleTimeString("es-CO", { hour: "2-digit", minute: "2-digit" });

// ---------- dark mode (persistido) ----------
function applyTheme(dark) {
  document.body.classList.toggle("dark", dark);
  $("themeToggle").textContent = dark ? "☀️" : "🌙";
  try { localStorage.setItem("gai-theme", dark ? "dark" : "light"); } catch (_) {}
}
$("themeToggle").addEventListener("click", () =>
  applyTheme(!document.body.classList.contains("dark")));
let initialDark = false;
try { initialDark = localStorage.getItem("gai-theme") === "dark"; } catch (_) {}
applyTheme(initialDark);

// ---------- capacidades ----------
async function loadCaps() {
  try {
    const r = await fetch("/api/capabilities");
    const c = await r.json();
    if (c.whatsapp) {
      $("waDot").classList.add("on");
      $("waMode").textContent = "Kapso conectado";
    } else {
      $("waMode").textContent = "Modo demo (sin Kapso)";
    }
    if (!c.llm && !c.colsubsidio) {
      $("waMode").textContent = "Motor no configurado";
      $("waStart").disabled = true;
    }
  } catch (_) { /* offline: queda en modo demo */ }
}

// ---------- render de burbujas ----------
function clearThread() {
  const t = $("waThread");
  t.classList.remove("empty");
  t.innerHTML = "";
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function addBubble(role, text) {
  removeTyping();
  const t = $("waThread");
  const b = document.createElement("div");
  b.className = "bubble " + (role === "agent" ? "agent" : "user");
  const who = role === "agent" ? "Guardian AI 🛡️" : "Cliente";
  b.innerHTML = `<span class="who">${who}</span>${escapeHTML(text)}<span class="t">${now()}</span>`;
  t.appendChild(b);
  t.scrollTop = t.scrollHeight;
}

function showTyping() {
  removeTyping();
  const t = $("waThread");
  if (t.classList.contains("empty")) return;
  typingEl = document.createElement("div");
  typingEl.className = "bubble agent typing";
  typingEl.innerHTML = "<span></span><span></span><span></span>";
  t.appendChild(typingEl);
  t.scrollTop = t.scrollHeight;
}
function removeTyping() { if (typingEl) { typingEl.remove(); typingEl = null; } }

// ---------- panel de eventos en vivo ----------
const EVENT_META = {
  CALL_STARTED:             { icon: "📞", cls: "blue",   label: "Conversación iniciada", detail: p => `canal ${p.channel || "whatsapp"}` },
  MESSAGE_RECEIVED:         { icon: "📩", cls: "green",  label: "Mensaje entrante" },
  FEATURE_UPDATED:          { icon: "⚙️", cls: "purple", label: "Perfil actualizado",   detail: p => `${p.key} = ${fmtVal(p.value)}` },
  INTENT_DETECTED:          { icon: "🎯", cls: "blue",   label: "Intención detectada",  detail: p => `${p.intent} · conf. ${pct(p.confidence)}` },
  LLM_REQUESTED:            { icon: "🧠", cls: "muted",  label: "Motor pensando…" },
  LLM_RESPONSE:             { icon: "💬", cls: "blue",   label: "Respuesta del motor",  detail: p => p.tokens_in ? `${p.tokens_in}→${p.tokens_out} tok · ${p.latency_ms}ms` : "" },
  TOOL_CALLED:              { icon: "🔧", cls: "muted",  label: "Herramienta",          detail: p => p.tool },
  TOOL_EXECUTED:            { icon: "✅", cls: "muted",  label: "Herramienta ejecutada", detail: p => p.tool },
  RECOMMENDATION_GENERATED: { icon: "⭐", cls: "amber",  label: "Recomendación generada", detail: p => p.product_name || p.product_id },
  MESSAGE_SENT:             { icon: "📤", cls: "green",  label: "Mensaje enviado",      detail: p => p.status === "queued" ? "entregado a Kapso" : p.status },
  SUMMARY_GENERATED:        { icon: "📝", cls: "blue",   label: "Resumen generado" },
  CALL_ENDED:               { icon: "🏁", cls: "red",    label: "Conversación finalizada", detail: p => p.reason },
  ERROR_OCCURRED:           { icon: "⚠️", cls: "red",    label: "Error recuperable",    detail: p => (p.message || "").slice(0, 90) },
  // STATE_CHANGED se omite del feed (ruido).
};
function fmtVal(v) { return typeof v === "boolean" ? (v ? "sí" : "no") : String(v); }
function pct(c) { return c ? Math.round(c * 100) + "%" : "—"; }

function clearFeed() {
  const f = $("evFeed");
  f.classList.remove("empty");
  f.innerHTML = "";
  $("evLive").style.display = "";
}

function addEvent(ev) {
  const meta = EVENT_META[ev.type];
  if (!meta) return;
  const f = $("evFeed");
  if (f.classList.contains("empty")) clearFeed();
  const d = meta.detail ? meta.detail(ev.payload || {}) : "";
  const el = document.createElement("div");
  el.className = `ev-item ev-c-${meta.cls}`;
  el.innerHTML = `<span class="ev-icon">${meta.icon}</span>
    <span class="ev-body"><span class="ev-label">${meta.label}</span>
    ${d ? `<div class="ev-detail">${escapeHTML(d)}</div>` : ""}</span>
    <span class="ev-time">${now()}</span>`;
  f.appendChild(el);
  while (f.children.length > 120) f.firstChild.remove();
  f.scrollTop = f.scrollHeight;
}

// ---------- estado de la UI ----------
function setUIActive(active) {
  $("waMsg").disabled = !active;
  $("waSend").disabled = !active;
  $("waEnd").disabled = !active;
  $("waStart").disabled = active;
  $("waPhone").disabled = active; // destino fijo durante la conversación
  if (active) $("waMsg").focus();
}

// Attach a una conversación; con replay reconstruye hilo + feed desde el
// EventStore (sobrevive a reloads y a conversaciones iniciadas fuera de la UI).
async function attachTo(id, { replay = true } = {}) {
  convID = id;
  if (DEBUG) console.log("[chat] attach", id);
  setUIActive(true);
  if (!replay) return;
  try {
    const r = await fetch(`/api/calls/${id}/events`);
    const evs = await r.json();
    clearThread();
    clearFeed();
    for (const ev of evs) {
      if (ev.type === "TRANSCRIPT_UPDATED" && ev.payload && ev.payload.is_final) {
        addBubble(ev.payload.role, ev.payload.text);
      } else {
        addEvent(ev);
      }
    }
  } catch (e) {
    console.warn("[chat] replay failed", e);
  }
}

// Al cargar: si hay una sesión WhatsApp viva, re-engancharse a ella.
async function tryReattach() {
  try {
    const r = await fetch("/api/whatsapp/sessions");
    const list = await r.json();
    if (Array.isArray(list) && list.length) {
      const s = list[list.length - 1];
      phone = s.phone;
      $("waPhone").value = s.phone;
      await attachTo(s.conversation_id);
    }
  } catch (_) { /* sin sesiones o backend viejo: queda en blanco */ }
}

// ---------- WebSocket: única fuente de verdad ----------
function setWS(on, txt) {
  $("wsDot").classList.toggle("on", on);
  $("wsDot").classList.toggle("pulse", on);
  $("wsState").textContent = txt;
}

function connectWS() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => setWS(true, "en vivo");
  ws.onmessage = (m) => {
    let ev;
    try { ev = JSON.parse(m.data); } catch (_) { return; }
    // Auto-attach: conversación WhatsApp nueva creada fuera de esta UI
    // (p.ej. mensaje real del celular vía Kapso) y no estamos siguiendo ninguna.
    if (!convID && ev.type === "CALL_STARTED" && ev.payload && ev.payload.channel === "whatsapp") {
      phone = ev.payload.from || phone;
      if (phone) $("waPhone").value = phone;
      attachTo(ev.call_id, { replay: true });
      return;
    }
    if (!convID || ev.call_id !== convID) return;
    addEvent(ev);
    if (ev.type === "MESSAGE_RECEIVED") showTyping();
    if (ev.type === "TRANSCRIPT_UPDATED" && ev.payload && ev.payload.is_final) {
      addBubble(ev.payload.role, ev.payload.text);
    }
    if (ev.type === "CALL_ENDED") removeTyping();
  };
  ws.onclose = () => { setWS(false, "reconectando…"); setTimeout(connectWS, 1500); };
  ws.onerror = () => setWS(false, "reconectando…");
}

// ---------- acciones ----------
async function startContact() {
  phone = $("waPhone").value.trim();
  if (!phone) { alert("Ingresa el teléfono del cliente."); return; }
  $("waStart").disabled = true;
  try {
    const r = await fetch("/api/chat/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ to: phone }),
    });
    if (!r.ok) { alert("No se pudo iniciar (¿motor configurado?)."); $("waStart").disabled = false; return; }
    const d = await r.json();
    clearThread();
    clearFeed();
    attachTo(d.conversation_id, { replay: false });
    showTyping(); // el saludo llega por WS
  } catch (e) {
    alert("Error de red iniciando el contacto.");
    $("waStart").disabled = false;
  }
}

async function sendInbound() {
  const text = $("waMsg").value.trim();
  if (!text || !convID) return;
  $("waMsg").value = "";
  showTyping();
  try {
    const r = await fetch("/api/whatsapp/simulate-inbound", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ from: phone, text }),
    });
    if (!r.ok) { removeTyping(); alert("Fallo al enviar el mensaje entrante."); }
  } catch (e) {
    removeTyping();
    alert("Error de red enviando el mensaje.");
  }
  // Las burbujas (cliente y respuesta del agente) llegan por WebSocket.
}

async function endContact() {
  if (!convID) return;
  try {
    await fetch(`/api/calls/${convID}/end`, { method: "POST" });
  } catch (_) { /* la UI se resetea igual */ }
  removeTyping();
  addBubble("agent", "— Conversación finalizada. Disponible en el Pipeline. —");
  setUIActive(false);
  $("waPhone").disabled = false;
  $("evLive").style.display = "none";
  convID = null;
}

$("waStart").addEventListener("click", startContact);
$("waSend").addEventListener("click", sendInbound);
$("waEnd").addEventListener("click", endContact);
$("waMsg").addEventListener("keydown", (e) => { if (e.key === "Enter") sendInbound(); });

loadCaps();
tryReattach();
connectWS();
