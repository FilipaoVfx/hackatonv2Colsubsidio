// Chat WhatsApp — multi-conversación en vivo + panel de eventos + dark mode.
// El asesor autónomo atiende N clientes en paralelo (el backend ya los aísla
// por call_id); esta vista los presenta como pestañas y guarda el historial de
// eventos POR conversación, así cambiar de cliente re-renderiza su hilo sin
// perder nada. Fuente de verdad única: el stream /ws.
const $ = (id) => document.getElementById(id);

// convs: convID -> {phone, events[], unread, lastAt, state}
const convs = new Map();
let activeID = null;   // conversación mostrada en pantalla
let typingEl = null;
let replaying = false; // durante el replay no animamos "escribiendo…"

const EVENTS_MAX = 400; // cap de historial por conversación

const now = () => new Date().toLocaleTimeString("es-CO", { hour: "2-digit", minute: "2-digit" });
const hhmm = (d) => new Date(d).toLocaleTimeString("es-CO", { hour: "2-digit", minute: "2-digit" });

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

// Re-enganche tras recargar: repuebla las pestañas con las sesiones vivas y
// abre la más reciente. Los mensajes siguientes llegan por /ws.
async function reattachLive() {
  try {
    const r = await fetch("/api/whatsapp/debug");
    if (!r.ok) return;
    const d = await r.json();
    const live = (d.live_sessions || []).sort((a, b) =>
      new Date(b.last_activity) - new Date(a.last_activity));
    live.forEach((s) => ensureConv(s.conversation_id, s.phone, s.last_activity));
    renderTabs();
    if (live.length && !activeID) selectConv(live[0].conversation_id);
  } catch (_) { /* sin debug endpoint: nada */ }
}

// ---------- registro de conversaciones ----------
function ensureConv(id, phone, at) {
  let c = convs.get(id);
  if (!c) {
    c = { phone: phone || "", events: [], unread: 0, lastAt: at || Date.now(), state: "" };
    convs.set(id, c);
  }
  if (phone && !c.phone) c.phone = phone;
  return c;
}

function renderTabs() {
  const wrap = $("convTabs");
  wrap.innerHTML = "";
  wrap.style.display = convs.size ? "" : "none";
  [...convs.entries()]
    .sort((a, b) => new Date(b[1].lastAt) - new Date(a[1].lastAt))
    .forEach(([id, c]) => {
      const el = document.createElement("button");
      el.className = "conv-tab" + (id === activeID ? " active" : "");
      el.innerHTML = `<span class="ct-phone">${escapeHTML(c.phone || id.slice(0, 8))}</span>
        <span class="ct-meta">${c.state ? escapeHTML(c.state) : "en curso"}</span>
        ${c.unread ? `<span class="ct-badge">${c.unread}</span>` : ""}`;
      el.addEventListener("click", () => selectConv(id));
      wrap.appendChild(el);
    });
}

// selectConv cambia la conversación visible y re-renderiza su hilo completo
// desde el historial guardado (nada se pierde al alternar entre clientes).
function selectConv(id) {
  const c = convs.get(id);
  if (!c) return;
  activeID = id;
  c.unread = 0;
  clearThread();
  clearFeed();
  resetLeadState();
  activateUI();
  $("waPhone").value = c.phone || "";
  replaying = true;
  c.events.forEach(handleEvent);
  replaying = false;
  removeTyping();
  renderTabs();
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

function addBubble(role, text, at) {
  removeTyping();
  const t = $("waThread");
  const b = document.createElement("div");
  b.className = "bubble " + (role === "agent" ? "agent" : "user");
  const who = role === "agent" ? "Guardian AI 🛡️" : "Cliente";
  b.innerHTML = `<span class="who">${who}</span>${escapeHTML(text)}<span class="t">${at ? hhmm(at) : now()}</span>`;
  t.appendChild(b);
  t.scrollTop = t.scrollHeight;
}

function showTyping() {
  if (replaying) return;
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
  // STATE_CHANGED se omite del feed (ruido); alimenta el stepper del lead.
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
    <span class="ev-time">${ev.timestamp ? hhmm(ev.timestamp) : now()}</span>`;
  f.appendChild(el);
  while (f.children.length > 120) f.firstChild.remove();
  f.scrollTop = f.scrollHeight;
}

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

// handleEvent renderiza un evento de la conversación ACTIVA (en vivo o replay).
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
      if (p.is_final) {
        if (p.role === "agent") removeTyping();
        addBubble(p.role, p.text, ev.timestamp);
        if (p.role === "user") showTyping(); // el agente va a responder
      }
      break;
    case "CALL_ENDED":
    case "ERROR_OCCURRED":
      removeTyping();
      break;
  }
}

// ---------- WebSocket: única fuente de verdad ----------
function setWS(on, txt) {
  $("wsDot").classList.toggle("on", on);
  $("wsDot").classList.toggle("pulse", on);
  $("wsState").textContent = txt;
}

// leadStateOf extrae el estado comercial para mostrarlo en la pestaña.
function leadStateOf(ev) {
  const to = (ev.payload || {}).to;
  return LEAD_ORDER.includes(to) || to === "COMPLETED" ? to : "";
}

function connectWS() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => setWS(true, "en vivo");
  ws.onmessage = (m) => {
    let ev;
    try { ev = JSON.parse(m.data); } catch (_) { return; }
    if (!ev.call_id) return;

    const p = ev.payload || {};
    // Solo seguimos conversaciones de WhatsApp: la primera señal es CALL_STARTED
    // con channel=whatsapp; los eventos previos a conocerla se guardan igual
    // (el backend publica el saludo antes de responder /api/chat/start).
    const known = convs.has(ev.call_id);
    if (!known && !(ev.type === "CALL_STARTED" && p.channel === "whatsapp")) {
      // evento de una conversación que aún no identificamos como WhatsApp:
      // guardamos en cuarentena por si CALL_STARTED llega después.
      pending.set(ev.call_id, [...(pending.get(ev.call_id) || []), ev].slice(-EVENTS_MAX));
      return;
    }

    const c = ensureConv(ev.call_id, p.from || p.to || "", ev.timestamp);
    if (!known && pending.has(ev.call_id)) {
      c.events.push(...pending.get(ev.call_id)); // rescata lo que llegó antes
      pending.delete(ev.call_id);
    }
    c.events.push(ev);
    if (c.events.length > EVENTS_MAX) c.events.shift();
    c.lastAt = ev.timestamp || Date.now();
    const st = leadStateOf(ev);
    if (st) c.state = st;
    if (!c.phone && p.to) c.phone = p.to;

    // Auto-selección: la primera conversación WhatsApp que aparezca se abre sola
    // (webhook real de Kapso o simulación). Las siguientes esperan en pestañas.
    if (!activeID) { selectConv(ev.call_id); return; }

    if (ev.call_id === activeID) {
      handleEvent(ev);
      renderTabsThrottled();
    } else {
      if (ev.type === "TRANSCRIPT_UPDATED" && p.is_final) c.unread++;
      renderTabsThrottled();
    }
  };
  ws.onclose = () => { setWS(false, "reconectando…"); setTimeout(connectWS, 1500); };
  ws.onerror = () => setWS(false, "reconectando…");
}

// eventos de conversaciones aún no identificadas como WhatsApp
const pending = new Map();

let tabsTimer = null;
function renderTabsThrottled() {
  if (tabsTimer) return;
  tabsTimer = setTimeout(() => { tabsTimer = null; renderTabs(); }, 250);
}

// ---------- acciones ----------
function activateUI() {
  $("waMsg").disabled = false;
  $("waSend").disabled = false;
  $("waEnd").disabled = false;
}

async function startContact() {
  const to = $("waPhone").value.trim();
  if (!to) { alert("Ingresa el teléfono del cliente."); return; }
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
  ensureConv(d.conversation_id, to, Date.now());
  selectConv(d.conversation_id);
  if ($("waThread").children.length === 0) showTyping(); // saludo en camino por WS
  $("waStart").disabled = false; // se puede abrir otra conversación en paralelo
  $("waMsg").focus();
}

async function sendInbound() {
  const text = $("waMsg").value.trim();
  const c = convs.get(activeID);
  if (!text || !c) return;
  $("waMsg").value = "";
  showTyping();
  const r = await fetch("/api/whatsapp/simulate-inbound", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ from: c.phone, text }),
  });
  if (!r.ok) { removeTyping(); alert("Fallo al enviar el mensaje entrante."); }
  // Las burbujas (cliente y respuesta del agente) llegan por WebSocket.
}

async function endContact() {
  if (!activeID) return;
  const id = activeID;
  await fetch(`/api/calls/${id}/end`, { method: "POST" });
  removeTyping();
  addBubble("agent", "— Conversación finalizada. Disponible en el Pipeline. —");
  convs.delete(id);
  activeID = null;
  $("waMsg").disabled = true;
  $("waSend").disabled = true;
  $("waEnd").disabled = true;
  $("waStart").disabled = false;
  $("evLive").style.display = "none";
  // abre la siguiente conversación viva, si queda alguna
  const next = [...convs.keys()][0];
  if (next) selectConv(next); else renderTabs();
}

$("waStart").addEventListener("click", startContact);
$("waSend").addEventListener("click", sendInbound);
$("waEnd").addEventListener("click", endContact);
$("waMsg").addEventListener("keydown", (e) => { if (e.key === "Enter") sendInbound(); });

loadCaps();
connectWS();
reattachLive();
