// Chat WhatsApp — hilo en vivo + panel de eventos en tiempo real + dark mode.
// Reusa el pipeline del backend (channel="whatsapp") y el stream /ws. Aditivo:
// no toca el demo de voz ni Mission Control.
const $ = (id) => document.getElementById(id);

let convID = null;
let phone = "";
let typingEl = null;

// preBuffer guarda los eventos que llegan por /ws ANTES de que el frontend
// conozca el convID (el backend publica CALL_STARTED + saludo de forma síncrona,
// antes de responder /api/chat/start). Sin esto el saludo se pierde y el hilo
// arranca vacío. Al fijar convID se re-reproducen (replay) los buffered de esa
// conversación, en orden. Fuente de verdad única = /ws, sin carreras.
let preBuffer = [];
const PREBUFFER_MAX = 400;

// liveMonitor: con la UI ociosa, engancharse automáticamente a cualquier
// conversación de WhatsApp que arranque (CALL_STARTED channel=whatsapp), venga
// del webhook REAL de Kapso (el cliente escribe primero) o de la simulación. Así
// lo que pasa en WhatsApp real se refleja en vivo en la web sin que el operador
// tenga que "iniciar contacto" primero.
let liveMonitor = true;

const now = () => new Date().toLocaleTimeString("es-CO", { hour: "2-digit", minute: "2-digit" });

// ---------- dark mode (persistido) ----------
function applyTheme(dark) {
  document.body.classList.toggle("dark", dark);
  $("themeToggle").textContent = dark ? "☀️" : "🌙";
  localStorage.setItem("gai-theme", dark ? "dark" : "light");
}
$("themeToggle").addEventListener("click", () =>
  applyTheme(!document.body.classList.contains("dark")));
applyTheme(localStorage.getItem("gai-theme") === "dark");

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
  KNOWLEDGE_RETRIEVED:      { icon: "📚", cls: "purple", label: "Consulta documental",   detail: p => (p.chunks || []).map(c => c.heading).join(", ") + ` (${p.mode})` },
  TURN_COMPLETED:           { icon: "⏱️", cls: "muted",  label: "Turno completado",      detail: p => `${p.state || ""} · ${(p.tool_calls || []).length} tools · ${p.latency_ms_total}ms` },
  LEAD_READY:               { icon: "🏆", cls: "amber",  label: "LEAD LISTO PARA ASESOR", detail: p => p.summary },
  MESSAGE_SENT:             { icon: "📤", cls: "green",  label: "Mensaje enviado",      detail: p => p.status === "queued" ? "entregado a Kapso" : p.status },
  SUMMARY_GENERATED:        { icon: "📝", cls: "blue",   label: "Resumen generado" },
  CALL_ENDED:               { icon: "🏁", cls: "red",    label: "Conversación finalizada", detail: p => p.reason },
  ERROR_OCCURRED:           { icon: "⚠️", cls: "red",    label: "Error recuperable",    detail: p => (p.message || "").slice(0, 90) },
  // STATE_CHANGED se omite del feed (ruido); se usa para el indicador "pensando".
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

// ---------- WebSocket: única fuente de verdad ----------
function setWS(on, txt) {
  $("wsDot").classList.toggle("on", on);
  $("wsDot").classList.toggle("pulse", on);
  $("wsState").textContent = txt;
}

// handleEvent renderiza un evento ya confirmado como perteneciente a convID.
// El indicador "escribiendo…" se ata al pensar REAL del motor: aparece cuando
// entra un mensaje / el motor arranca, y se retira al llegar la burbuja del
// agente, al finalizar o ante un error. Así la UI se siente reactiva durante la
// latencia del LLM en vez de parpadear.
// ---------- Guardian: stepper de estados + perfil en vivo ----------
const LEAD_ORDER = ["AFFILIATION_CHECK", "PROFILE_DISCOVERY", "FINANCIAL_QUALIFICATION",
  "PROJECT_MATCHING", "READY_FOR_ADVISOR", "NURTURING"];

function setLeadState(to) {
  if (!LEAD_ORDER.includes(to) && to !== "COMPLETED" && to !== "NEW") return;
  const wrap = $("leadSteps");
  wrap.style.display = "";
  const idx = LEAD_ORDER.indexOf(to);
  wrap.querySelectorAll(".lead-step").forEach((el) => {
    const i = LEAD_ORDER.indexOf(el.dataset.st);
    el.classList.remove("now", "done");
    if (to === "COMPLETED") { el.classList.add(el.dataset.st === "NURTURING" ? "now" : "done"); return; }
    if (i < idx) el.classList.add("done");
    if (i === idx) el.classList.add("now");
  });
}

function resetLeadState() {
  $("leadSteps").style.display = "none";
  $("leadSteps").querySelectorAll(".lead-step").forEach((el) => el.classList.remove("now", "done"));
  const pr = $("profileRows");
  pr.classList.add("empty");
  pr.textContent = "Las variables confirmadas del cliente aparecerán aquí al instante.";
}

function addProfileVar(key, value) {
  const pr = $("profileRows");
  if (pr.classList.contains("empty")) { pr.classList.remove("empty"); pr.innerHTML = ""; }
  let row = pr.querySelector(`[data-k="${CSS.escape(key)}"]`);
  if (!row) {
    row = document.createElement("div");
    row.className = "profile-row";
    row.dataset.k = key;
    row.innerHTML = `<span class="k">${escapeHTML(key)}</span><span class="v"></span>`;
    pr.appendChild(row);
  }
  row.querySelector(".v").textContent = fmtVal(value);
}

function showLeadReady(p) {
  removeTyping();
  const t = $("waThread");
  const card = document.createElement("div");
  card.className = "lead-ready";
  const recs = (p.recommendations || []).map((r) => `• ${escapeHTML(r)}`).join("<br>");
  card.innerHTML = `<b>🏆 Lead listo para asesor</b>${escapeHTML(p.summary || "")}${recs ? "<br>" + recs : ""}`;
  t.appendChild(card);
  t.scrollTop = t.scrollHeight;
}

function handleEvent(ev) {
  addEvent(ev);
  const p = ev.payload || {};
  if (ev.type === "STATE_CHANGED" && p.to) setLeadState(p.to);
  if (ev.type === "FEATURE_UPDATED" && p.key) addProfileVar(p.key, p.value);
  if (ev.type === "LEAD_READY") showLeadReady(p);
  switch (ev.type) {
    case "MESSAGE_RECEIVED":
    case "LLM_REQUESTED":
      showTyping();
      break;
    case "TRANSCRIPT_UPDATED":
      if (ev.payload && ev.payload.is_final) {
        if (ev.payload.role === "agent") removeTyping();
        addBubble(ev.payload.role, ev.payload.text);
        if (ev.payload.role === "user") showTyping(); // el agente va a responder
      }
      break;
    case "CALL_ENDED":
    case "ERROR_OCCURRED":
      removeTyping();
      break;
  }
}

// drainPreBuffer re-reproduce, en orden, los eventos de esta conversación que
// llegaron antes de conocer convID (p.ej. el saludo de apertura).
function drainPreBuffer() {
  const buffered = preBuffer.filter((e) => e.call_id === convID);
  preBuffer = [];
  buffered.forEach(handleEvent);
}

// activateUI habilita compose/finalizar y bloquea "iniciar" mientras hay una
// conversación enganchada.
function activateUI() {
  $("waMsg").disabled = false;
  $("waSend").disabled = false;
  $("waEnd").disabled = false;
  $("waStart").disabled = true;
}

// attach conecta la UI a una conversación por su call_id. Idempotente: lo usan
// tanto el contacto saliente manual como el auto-enganche a WhatsApp real. Si ya
// hay otra conversación activa, ignora (panel de una conversación a la vez).
function attach(callID, phoneNum, opts = {}) {
  if (convID === callID) return;            // ya enganchados a esta
  if (convID && convID !== callID) return;  // ocupados con otra
  convID = callID;
  if (phoneNum) { phone = phoneNum; $("waPhone").value = phoneNum; }
  clearThread();
  clearFeed();
  resetLeadState();
  activateUI();
  drainPreBuffer(); // re-reproduce lo que llegó antes de engancharnos
  if (opts.live) addBubble("agent", `— Conversación en vivo con ${phone || "cliente"} —`);
}

function connectWS() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => setWS(true, "en vivo");
  ws.onmessage = (m) => {
    let ev;
    try { ev = JSON.parse(m.data); } catch (_) { return; }
    // Auto-enganche: UI ociosa + arranca una conversación de WhatsApp = adoptarla
    // en vivo (webhook real de Kapso o simulación). Este es el puente que hace
    // que lo de WhatsApp real se vea en la web sin "iniciar contacto".
    if (!convID && liveMonitor && ev.type === "CALL_STARTED" &&
        ev.payload && ev.payload.channel === "whatsapp") {
      attach(ev.call_id, ev.payload.from || "", { live: true });
    }
    if (!convID) {
      // Aún no sabemos a qué conversación pertenecemos: bufferizar por si es la
      // nuestra (se filtra por call_id al fijar convID).
      preBuffer.push(ev);
      if (preBuffer.length > PREBUFFER_MAX) preBuffer.shift();
      return;
    }
    if (ev.call_id !== convID) return;
    handleEvent(ev);
  };
  ws.onclose = () => { setWS(false, "reconectando…"); setTimeout(connectWS, 1500); };
  ws.onerror = () => setWS(false, "reconectando…");
}

// ---------- acciones ----------
async function startContact() {
  const to = $("waPhone").value.trim();
  if (!to) { alert("Ingresa el teléfono del cliente."); return; }
  phone = to;
  $("waStart").disabled = true;
  let d;
  try {
    const r = await fetch("/api/chat/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ to }),
    });
    if (!r.ok) throw new Error("start failed");
    d = await r.json();
  } catch (_) {
    alert("No se pudo iniciar (¿motor configurado?)."); $("waStart").disabled = false; return;
  }
  attach(d.conversation_id, to); // no-op si el auto-enganche ya adoptó esta conv
  if ($("waThread").children.length === 0) showTyping(); // saludo aún en camino por WS
  $("waMsg").focus();
}

async function sendInbound() {
  const text = $("waMsg").value.trim();
  if (!text || !convID) return;
  $("waMsg").value = "";
  showTyping();
  const r = await fetch("/api/whatsapp/simulate-inbound", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ from: phone, text }),
  });
  if (!r.ok) { removeTyping(); alert("Fallo al enviar el mensaje entrante."); }
  // Las burbujas (cliente y respuesta del agente) llegan por WebSocket.
}

async function endContact() {
  if (!convID) return;
  await fetch(`/api/calls/${convID}/end`, { method: "POST" });
  removeTyping();
  addBubble("agent", "— Conversación finalizada. Disponible en el Pipeline. —");
  $("waMsg").disabled = true;
  $("waSend").disabled = true;
  $("waEnd").disabled = true;
  $("waStart").disabled = false;
  $("evLive").style.display = "none";
  convID = null;
  preBuffer = []; // evita fugas de eventos hacia la próxima conversación
  // el stepper/perfil quedan visibles hasta el próximo attach (lectura post-mortem)
}

$("waStart").addEventListener("click", startContact);
$("waSend").addEventListener("click", sendInbound);
$("waEnd").addEventListener("click", endContact);
$("waMsg").addEventListener("keydown", (e) => { if (e.key === "Enter") sendInbound(); });

loadCaps();
connectWS();
