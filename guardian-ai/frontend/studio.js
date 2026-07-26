// Agent Studio — consola de configuración del asesor (fases 1 a 4 del plan
// 10_PLAN_AGENT_STUDIO.md).
//
// Principio del PRD: aquí no se escriben prompts. Se mueven controles de un
// vocabulario cerrado y el backend compone las instrucciones. Por eso esta
// pantalla NO tiene ningún campo de texto libre salvo el nombre del agente, y
// las frases que se muestran bajo cada control vienen del backend: si se
// escribieran aquí, un día dirían una cosa y el modelo recibiría otra.
const STUDIO_UI_VERSION = "1.3.0";
console.log(`[guardian-ai] studio UI v${STUDIO_UI_VERSION}`);

const $ = (id) => document.getElementById(id);

let cfg = null;        // configuración en edición (local)
let catalogs = null;
let published = null;  // la que está atendiendo clientes AHORA

// Estado por sección. Cada sección tiene su propio botón "Aplicar cambios", que
// es el único gesto que cambia el comportamiento del asesor en vivo. Mientras no
// se pulse, lo que se mueve en pantalla no afecta a ninguna conversación — y la
// sección lo dice con "sin aplicar" en vez de dejar al usuario adivinando.
const SECTIONS = {
  general: "Nombre del agente",
  persona: "Personalidad",
  sales:   "Objetivos comerciales",
  safety:  "Límites de seguridad",
};
const pending = { general: false, persona: false, sales: false, safety: false };
const anyPending = () => Object.values(pending).some(Boolean);

// ---------- dark mode (mismo contrato que el resto de vistas) ----------
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

// ---------- carga ----------
async function load() {
  const r = await fetch("/api/studio/config");
  if (!r.ok) {
    $("statusChip").textContent = "consola no disponible";
    $("promptBox").textContent = "El Agent Studio necesita el motor Guardian activo.";
    return;
  }
  const data = await r.json();
  cfg = data.draft;
  published = data.published;
  catalogs = data.catalogs;

  if (data.store_error) {
    const box = $("storeError");
    box.hidden = false;
    box.innerHTML = `<strong>Configuración degradada</strong><span>${data.store_error} — el asesor está corriendo con los valores de fábrica.</span>`;
  }

  $("model").textContent = data.runtime.model;
  renderBytes(data.config_bytes, data.config_bytes_max);
  renderRuntime(data.runtime);
  renderStates(data.runtime.states);
  renderAll();
  refreshPrompt();
  loadVersions();
}

// El peso de la configuración dentro del prompt: mover controles cuesta tokens
// en CADA turno, y eso se ve aquí en vez de descubrirse en la factura.
function renderBytes(bytes, max) {
  if (max) cfgBytesMax = max;
  if (!bytes) return;
  $("configBytes").textContent = `${bytes} de ${cfgBytesMax} bytes`;
}

async function loadVersions() {
  const host = $("versions");
  try {
    const r = await fetch("/api/studio/versions");
    if (!r.ok) return;
    const { versions } = await r.json();
    host.innerHTML = "";
    if (!versions || !versions.length) {
      host.innerHTML = `<div class="version empty">Todavía no se ha publicado ninguna versión: el asesor corre con los valores de fábrica.</div>`;
      return;
    }
    for (const v of versions) host.appendChild(versionRow(v));
  } catch (_) { /* el historial es informativo: su fallo no rompe la consola */ }
}

function versionRow(v) {
  const row = document.createElement("div");
  row.className = "version";
  row.innerHTML = `<span class="v-num"></span><span class="v-note"></span><span class="v-date"></span>`;
  row.querySelector(".v-num").textContent = `v${v.version}`;
  const note = row.querySelector(".v-note");
  if (v.note) note.textContent = v.note;
  else note.innerHTML = `<em>sin nota</em>`;
  row.querySelector(".v-date").textContent = v.updated_at && !v.updated_at.startsWith("0001")
    ? new Date(v.updated_at).toLocaleDateString("es-CO")
    : "fábrica";

  const back = document.createElement("button");
  back.className = "btn-ghost";
  back.textContent = "Volver a esta";
  back.title = `Republicar la versión ${v.version} como una versión nueva`;
  back.addEventListener("click", () => rollback(v.version));
  row.appendChild(back);
  return row;
}

// ---------- render ----------
const SLIDERS = [
  ["empathy", "Empatía"],
  ["formality", "Formalidad"],
  ["closeness", "Cercanía"],
  ["persuasion", "Persuasión comercial"],
  ["proactivity", "Proactividad"],
];

// phraseFor elige la frase del tramo igual que el backend (1-3 / 4-7 / 8-10).
function phraseFor(knob, value) {
  const phrases = catalogs.persona_phrases[knob] || [];
  if (value <= 3) return phrases[0];
  if (value <= 7) return phrases[1];
  return phrases[2];
}

function renderAll() {
  $("agentName").value = cfg.persona.agent_name;
  $("sideAgentName").textContent = cfg.persona.agent_name || "Guardian";
  $("liveVersion").textContent = `v${published.version}`;
  $("updatedAt").textContent = cfg.updated_at && !cfg.updated_at.startsWith("0001")
    ? new Date(cfg.updated_at).toLocaleString("es-CO")
    : "sin cambios";
  renderStatusChip();

  renderSliders();
  $("emojis").checked = cfg.persona.emojis;
  $("humor").checked = cfg.persona.humor;
  renderLengths();
  renderGoals();
  renderForbid();
  renderLevels();
}

function renderSliders() {
  const host = $("sliders");
  host.innerHTML = "";
  for (const [key, label] of SLIDERS) {
    const ends = catalogs.persona_scales[key] || ["", ""];
    const value = cfg.persona[key];
    const wrap = document.createElement("div");
    wrap.className = "knob";
    wrap.innerHTML = `
      <div class="knob-head"><span class="label">${label}</span><span class="knob-value">${value}/10</span></div>
      <input type="range" min="1" max="10" step="1" value="${value}" id="sl-${key}"
             aria-label="${label}" aria-valuetext="${phraseFor(key, value)}" />
      <div class="knob-ends"><span>${ends[0]}</span><span>${ends[1]}</span></div>
      <div class="knob-phrase" id="ph-${key}">${phraseFor(key, value)}</div>`;
    host.appendChild(wrap);
    const input = $(`sl-${key}`);
    input.addEventListener("input", () => {
      const v = Number(input.value);
      cfg.persona[key] = v;
      wrap.querySelector(".knob-value").textContent = `${v}/10`;
      const phrase = phraseFor(key, v);
      $(`ph-${key}`).textContent = phrase;
      input.setAttribute("aria-valuetext", phrase);
      markDirty("persona");
    });
  }
}

function renderLengths() {
  const host = $("lengths");
  host.innerHTML = "";
  catalogs.lengths.forEach((len, i) => {
    const b = document.createElement("button");
    b.className = "btn-ghost";
    b.textContent = len[0].toUpperCase() + len.slice(1);
    b.setAttribute("aria-pressed", String(cfg.persona.length === len));
    b.addEventListener("click", () => { cfg.persona.length = len; renderLengths(); markDirty("persona"); });
    host.appendChild(b);
    if (cfg.persona.length === len) $("lengthPhrase").textContent = catalogs.persona_phrases.length[i];
  });
}

function renderGoals() {
  const host = $("goals");
  host.innerHTML = "";
  const active = cfg.sales.goals;
  const inactive = catalogs.sales_goals.filter((g) => !active.includes(g));

  active.forEach((goal, i) => host.appendChild(goalRow(goal, i, true, active.length)));
  inactive.forEach((goal) => host.appendChild(goalRow(goal, -1, false, active.length)));
}

function goalRow(goal, index, on, total) {
  const row = document.createElement("div");
  row.className = "goal" + (on ? "" : " off");
  row.innerHTML = `
    <span class="order">${on ? index + 1 : "—"}</span>
    <span class="name">${catalogs.goal_labels[goal] || goal}</span>
    <span class="moves"></span>`;
  const moves = row.querySelector(".moves");

  if (on) {
    moves.appendChild(moveBtn("↑", "Subir prioridad", index === 0, () => swapGoal(index, index - 1)));
    moves.appendChild(moveBtn("↓", "Bajar prioridad", index === total - 1, () => swapGoal(index, index + 1)));
    moves.appendChild(moveBtn("Quitar", "Quitar objetivo", total === 1, () => {
      cfg.sales.goals.splice(index, 1); renderGoals(); markDirty("sales");
    }));
  } else {
    moves.appendChild(moveBtn("Añadir", "Añadir objetivo", false, () => {
      cfg.sales.goals.push(goal); renderGoals(); markDirty("sales");
    }));
  }
  return row;
}

function moveBtn(text, title, disabled, onClick) {
  const b = document.createElement("button");
  b.className = "btn-ghost";
  b.textContent = text;
  b.title = title;
  b.disabled = disabled;
  b.addEventListener("click", onClick);
  return b;
}

function swapGoal(a, b) {
  const g = cfg.sales.goals;
  [g[a], g[b]] = [g[b], g[a]];
  renderGoals();
  markDirty("sales");
}

function renderForbid() {
  const host = $("forbid");
  host.innerHTML = "";
  catalogs.safety_forbid.forEach((key) => {
    const row = document.createElement("div");
    row.className = "switch-row";
    const id = `fb-${key}`;
    row.innerHTML = `<label for="${id}">${catalogs.forbid_labels[key] || key}</label>
                     <input type="checkbox" id="${id}" ${cfg.safety.forbid.includes(key) ? "checked" : ""} />`;
    host.appendChild(row);
    $(id).addEventListener("change", (e) => {
      if (e.target.checked) cfg.safety.forbid.push(key);
      else cfg.safety.forbid = cfg.safety.forbid.filter((k) => k !== key);
      markDirty("safety");
    });
  });
}

function renderLevels() {
  const host = $("levels");
  host.innerHTML = "";
  catalogs.safety_levels.forEach((level, i) => {
    const b = document.createElement("button");
    b.className = "btn-ghost";
    b.textContent = level[0].toUpperCase() + level.slice(1);
    b.setAttribute("aria-pressed", String(cfg.safety.level === level));
    b.addEventListener("click", () => { cfg.safety.level = level; renderLevels(); markDirty("safety"); });
    host.appendChild(b);
    if (cfg.safety.level === level) $("levelPhrase").textContent = catalogs.persona_phrases.safety_level[i];
  });
}

// El estado del motor se muestra en solo lectura: es más honesto que pintar
// interruptores que no mueven nada.
function renderRuntime(rt) {
  const rows = [
    ["Temperatura", rt.temperature],
    ["Ventana de memoria", `${rt.history_window} mensajes`],
    ["Documentación (RAG)", `${rt.rag_chunks} fragmentos · ${rt.rag_mode}`],
    ["Fragmentos por consulta", rt.rag_top_k],
    ["Umbral de confianza", rt.confidence_threshold],
    ["Reintentos de recomendación", rt.max_rec_attempts],
    ["Herramientas registradas", rt.tools.length],
    ["Etapas del lead", rt.states.length],
  ];
  $("runtime").innerHTML = rows.map(([k, v]) =>
    `<div class="field"><span class="k">${k}</span><span class="rule-fill"></span><span class="v mono">${v}</span></div>`
  ).join("");
}

function renderStates(states) {
  const sel = $("promptState");
  sel.innerHTML = states.map((s) => `<option value="${s}">${s}</option>`).join("");
  sel.value = "PROFILE_DISCOVERY";
  sel.addEventListener("change", refreshPrompt);
}

// ---------- prompt inspector ----------
async function refreshPrompt() {
  const state = $("promptState").value || "PROFILE_DISCOVERY";
  const r = await fetch(`/api/studio/prompt?draft=1&state=${encodeURIComponent(state)}`);
  if (!r.ok) return;
  const data = await r.json();
  $("promptBox").textContent = data.prompt;
  $("promptBytes").textContent = `${data.bytes} bytes · ${data.source}`;
}

$("copyBtn").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText($("promptBox").textContent);
    $("copyBtn").textContent = "Copiado";
    setTimeout(() => ($("copyBtn").textContent = "Copiar"), 1500);
  } catch (_) { /* sin portapapeles: el texto se puede seleccionar igual */ }
});

// ---------- edición ----------
$("agentName").addEventListener("input", (e) => {
  cfg.persona.agent_name = e.target.value;
  $("sideAgentName").textContent = e.target.value || "Guardian";
  markDirty("general");
});

// Emojis y humor: son interruptores del mismo modelo que el resto de la
// personalidad, así que escriben en la configuración igual que los sliders.
for (const knob of ["emojis", "humor"]) {
  $(knob).addEventListener("change", (e) => {
    cfg.persona[knob] = e.target.checked;
    markDirty("persona");
  });
}

// ---------- estado por sección ----------
const applyBtn = (section) => document.querySelector(`[data-apply="${section}"]`);
const stateEl  = (section) => document.querySelector(`[data-state="${section}"]`);

// El chip de cabecera responde siempre a la pregunta "¿qué está atendiendo
// clientes ahora mismo?", y avisa si hay algo escrito que todavía no lo hace.
function renderStatusChip() {
  const chip = $("statusChip");
  if (anyPending()) {
    const n = Object.values(pending).filter(Boolean).length;
    chip.textContent = `v${published.version} en vivo · ${n} secci${n === 1 ? "ón" : "ones"} sin aplicar`;
    return;
  }
  chip.textContent = `v${published.version} en vivo · ${published.persona.agent_name}`;
}

function markDirty(section) {
  pending[section] = true;
  const btn = applyBtn(section);
  if (btn) btn.disabled = false;
  setSectionState(section, "Sin aplicar: el asesor sigue con la versión anterior.", "pending");
  renderStatusChip();
}

function setSectionState(section, text, kind) {
  const el = stateEl(section);
  if (!el) return;
  el.textContent = text;
  el.className = "sec-state" + (kind ? " " + kind : "");
}

// Tras aplicar, TODAS las secciones quedan limpias: la configuración es un
// único objeto con una única versión, así que publicar una sección publica
// también lo que hubiera pendiente en las demás. La confirmación lo dice.
function clearAllPending() {
  for (const section of Object.keys(SECTIONS)) {
    pending[section] = false;
    const btn = applyBtn(section);
    if (btn) btn.disabled = true;
    setSectionState(section, "", "");
  }
}

// showFieldErrors marca el control que falla, en su sección. Un cartel genérico
// obligaría a adivinar cuál de veinte controles rompió la validación.
function showFieldErrors(errors) {
  $("agentNameErr").hidden = true;
  $("goalsErr").hidden = true;
  for (const err of errors) {
    if (err.field === "persona.agent_name") {
      $("agentNameErr").textContent = err.message;
      $("agentNameErr").hidden = false;
      setSectionState("general", err.message, "err");
    } else if (err.field === "sales.goals") {
      $("goalsErr").textContent = err.message;
      $("goalsErr").hidden = false;
      setSectionState("sales", err.message, "err");
    } else {
      // El resto de campos vive en personalidad o en límites.
      setSectionState(err.field.startsWith("safety") ? "safety" : "persona",
        `${err.field}: ${err.message}`, "err");
    }
  }
}

let cfgBytesMax = 2048; // se refresca desde el backend al cargar

// saveDraftOnly guarda sin aplicar. Lo usa el Playground: probar corre contra el
// borrador guardado, así que se puede ensayar un cambio sin que ningún cliente
// real lo note. NO toca el comportamiento en vivo.
async function saveDraftOnly() {
  const r = await fetch("/api/studio/config/draft", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cfg),
  });
  if (r.status === 422) {
    showFieldErrors((await r.json()).errors || []);
    return false;
  }
  if (!r.ok) return false;
  showFieldErrors([]);
  return true;
}

// ---------- aplicar (el gesto que cambia el asesor en vivo) ----------
//
// Un clic hace las dos cosas: guarda la configuración y la publica. El usuario
// no tiene por qué conocer la distinción interna entre borrador y publicado —
// pulsa "Aplicar cambios" en la sección que tocó y el asesor cambia.
//
// La configuración es UN objeto con UNA versión, así que aplicar desde una
// sección publica también lo que hubiera pendiente en las demás. La
// confirmación lo dice en vez de dejar un estado a medias e invisible.
async function applySection(section) {
  const btn = applyBtn(section);
  const others = Object.keys(pending).filter((s) => s !== section && pending[s]);

  btn.disabled = true;
  setSectionState(section, "Aplicando…", "");
  try {
    if (!(await saveDraftOnly())) {
      setSectionState(section, "No se aplicó: revisa los campos marcados.", "err");
      btn.disabled = false;
      return;
    }
    const note = others.length
      ? `${SECTIONS[section]} (+ ${others.map((s) => SECTIONS[s].toLowerCase()).join(", ")})`
      : SECTIONS[section];
    const r = await fetch("/api/studio/config/publish", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ note }),
    });
    if (r.status === 422) {
      showFieldErrors((await r.json()).errors || []);
      setSectionState(section, "No se aplicó: revisa los campos marcados.", "err");
      btn.disabled = false;
      return;
    }
    if (!r.ok) {
      setSectionState(section, "No se aplicó: el servidor rechazó el cambio.", "err");
      btn.disabled = false;
      return;
    }
    const body = await r.json();
    const extra = others.length ? ` (incluye ${others.map((s) => SECTIONS[s].toLowerCase()).join(" y ")})` : "";
    applied(body, section,
      `✓ Aplicado — v${body.version} en vivo${extra}. Las conversaciones abiertas la usan desde su siguiente mensaje.`);
  } catch (err) {
    setSectionState(section, `No se aplicó: ${err.message}`, "err");
    btn.disabled = false;
  }
}

// applied deja la consola contando la verdad tras un cambio en vivo: nueva
// versión, sin pendientes, prompt e historial al día.
function applied(body, section, message) {
  published = body.published;
  cfg = { ...body.published, status: "draft" };
  clearAllPending();
  renderAll();
  renderBytes(body.config_bytes, cfgBytesMax);
  refreshPrompt();
  loadVersions();
  if (section) {
    setSectionState(section, message, "ok");
    // El aviso se apaga, pero el chip de cabecera sigue diciendo qué versión
    // está viva: el feedback permanente no depende de un temporizador.
    setTimeout(() => {
      if (!pending[section]) setSectionState(section, "", "");
    }, 10000);
  }
}

document.querySelectorAll("[data-apply]").forEach((btn) =>
  btn.addEventListener("click", () => applySection(btn.dataset.apply)));

async function rollback(version) {
  const r = await fetch(`/api/studio/config/rollback/${version}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ note: `vuelta a la versión ${version}` }),
  });
  if (!r.ok) { $("statusChip").textContent = `no se pudo volver a v${version}`; return; }
  const body = await r.json();
  applied(body, "general", `✓ Recuperada v${version} como v${body.version}, ya en vivo.`);
}

// Restablecer NO aplica solo: carga los valores de fábrica en pantalla y deja
// las secciones pendientes, para que el cambio en vivo siga siendo un gesto
// deliberado.
$("resetBtn").addEventListener("click", async () => {
  const r = await fetch("/api/studio/config");
  const data = await r.json();
  cfg = data.defaults;
  cfg.status = "draft";
  renderAll();
  for (const section of Object.keys(SECTIONS)) markDirty(section);
  setSectionState("general", "Valores de fábrica cargados. Pulsa Aplicar para ponerlos en vivo.", "pending");
});

// ---------- pestañas de la columna derecha ----------
function showPanel(which) {
  const play = which === "play";
  $("panelPlay").hidden = !play;
  $("panelPrompt").hidden = play;
  $("tabPlay").setAttribute("aria-pressed", String(play));
  $("tabPrompt").setAttribute("aria-pressed", String(!play));
}
$("tabPlay").addEventListener("click", () => showPanel("play"));
$("tabPrompt").addEventListener("click", () => showPanel("prompt"));

// ---------- Playground ----------
// Corre en un mundo aparte: bus, sesiones, motor y API propios (fase 3 del
// plan). Aquí solo se pinta lo que devuelve, más los eventos que llegan en vivo
// por /ws/studio mientras el turno corre.
const SCENARIOS = [
  "Hola, ¿qué es esto?",
  "¿Cuánto cuesta?",
  "Tengo un perro y dos hijos",
  "Está muy caro",
  "Quiero hablar con un asesor",
];

let play = { enabled: false, session: null, busy: false };

async function loadPlayground() {
  let data = { enabled: false };
  try {
    const r = await fetch("/api/studio/playground");
    if (r.ok) data = await r.json();
  } catch (_) { /* consola sin backend: se declara apagado */ }

  play.enabled = !!data.enabled;
  if (!play.enabled) {
    $("playSeal").textContent = "no disponible";
    $("playHint").textContent =
      "El Playground necesita el motor Guardian y una API de pruebas (STUDIO_API_URL). " +
      "Mientras tanto puedes revisar el prompt generado en la pestaña Prompt.";
    $("playInput").disabled = true;
    $("playSend").disabled = true;
    $("playReset").disabled = true;
    return;
  }
  $("playSeal").textContent = "aislado";
  $("playHint").textContent =
    `Escribe como si fueras el cliente. Corre con el borrador contra ${data.api} y en un mundo aparte: ` +
    `no envía WhatsApp, no entra al pipeline y no toca las conversaciones vivas. Máximo ${data.max_turns} turnos por prueba.`;
  renderScenarios();
  connectStudioWS();
}

function renderScenarios() {
  const host = $("playScenarios");
  host.innerHTML = "";
  for (const text of SCENARIOS) {
    const b = document.createElement("button");
    b.className = "btn-ghost";
    b.textContent = text;
    b.addEventListener("click", () => { $("playInput").value = text; sendPlay(); });
    host.appendChild(b);
  }
}

function bubble(role, text, buttons) {
  const thread = $("playThread");
  thread.classList.remove("empty");
  const el = document.createElement("div");
  el.className = `bubble ${role}`;
  const who = document.createElement("span");
  who.className = "who";
  who.textContent = role === "user" ? "Cliente" : (cfg?.persona?.agent_name || "Agente");
  el.appendChild(who);
  el.appendChild(document.createTextNode(text));
  if (buttons && buttons.length) {
    const quick = document.createElement("div");
    quick.className = "quick";
    for (const b of buttons) {
      const s = document.createElement("span");
      s.textContent = b;
      quick.appendChild(s);
    }
    el.appendChild(quick);
  }
  thread.appendChild(el);
  thread.scrollTop = thread.scrollHeight;
  return el;
}

function typingBubble() {
  const thread = $("playThread");
  const el = document.createElement("div");
  el.className = "bubble agent typing";
  el.innerHTML = "<span></span><span></span><span></span>";
  thread.appendChild(el);
  thread.scrollTop = thread.scrollHeight;
  return el;
}

function renderPlayMeta(session) {
  $("playState").textContent = session.state || "—";
  $("playTurns").textContent = `${session.turns}/${session.turns + session.turns_left}`;
  $("playCost").textContent = `US$ ${(session.cost_usd || 0).toFixed(4)}`;
}

async function sendPlay() {
  const text = $("playInput").value.trim();
  if (!play.enabled || play.busy || !text) return;

  // Probar corre contra el borrador guardado, así que lo que hay en pantalla se
  // guarda primero. Guardar NO aplica: el asesor real sigue con su versión.
  if (anyPending() && !(await saveDraftOnly())) {
    $("statusChip").textContent = "corrige los campos marcados antes de probar";
    return;
  }

  play.busy = true;
  $("playSend").disabled = true;
  $("playInput").value = "";
  bubble("user", text);
  const typing = typingBubble();
  $("playEvents").innerHTML = "";

  try {
    if (!play.session) {
      const r = await fetch("/api/studio/playground/start", { method: "POST" });
      if (!r.ok) throw new Error("no se pudo abrir la sesión de prueba");
      play.session = await r.json();
    }
    const r = await fetch("/api/studio/playground/message", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id: play.session.session_id, text }),
    });
    typing.remove();
    if (r.status === 429) {
      bubble("agent", "Se acabaron los turnos de esta prueba. Reinicia para seguir probando.");
      return;
    }
    if (!r.ok) {
      bubble("agent", `El turno de prueba falló: ${await r.text()}`);
      return;
    }
    const turn = await r.json();
    play.session = turn.session;
    bubble("agent", turn.reply, turn.buttons);
    renderPlayMeta(turn.session);
    // El resumen se añade al final del feed en vivo, no lo sustituye: lo que
    // pasó durante el turno sigue a la vista.
    const summary = document.createElement("div");
    summary.innerHTML = `<span class="t"></span><span class="d"></span>`;
    summary.querySelector(".t").textContent = "turno";
    summary.querySelector(".d").textContent =
      `config v${turn.config_version} · ${turn.latency_ms} ms · US$ ${(turn.turn_cost_usd || 0).toFixed(4)} · ${turn.events.length} eventos`;
    $("playEvents").appendChild(summary);
  } catch (err) {
    typing.remove();
    bubble("agent", `El turno de prueba falló: ${err.message}`);
  } finally {
    play.busy = false;
    $("playSend").disabled = false;
  }
}

$("playSend").addEventListener("click", sendPlay);
$("playInput").addEventListener("keydown", (e) => { if (e.key === "Enter") sendPlay(); });

$("playReset").addEventListener("click", async () => {
  if (play.session) {
    await fetch("/api/studio/playground/reset", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id: play.session.session_id }),
    }).catch(() => {});
  }
  play.session = null;
  const thread = $("playThread");
  thread.innerHTML = "<span>Sin conversación de prueba todavía.</span>";
  thread.classList.add("empty");
  $("playEvents").innerHTML = "";
  $("playState").textContent = "—";
  $("playTurns").textContent = "—";
  $("playCost").textContent = "—";
});

// Actividad del motor en vivo. Es un extra: si el WebSocket no conecta, el
// turno se sigue viendo entero en la respuesta HTTP.
function connectStudioWS() {
  let ws;
  try {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    ws = new WebSocket(`${proto}://${location.host}/ws/studio`);
  } catch (_) { return; }
  ws.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch (_) { return; }
    const host = $("playEvents");
    const row = document.createElement("div");
    const detail = ev.payload?.tool || ev.payload?.to || ev.payload?.key ||
                   ev.payload?.intent || ev.payload?.strategy || ev.producer || "";
    row.innerHTML = `<span class="t"></span><span class="d"></span>`;
    row.querySelector(".t").textContent = ev.type;
    row.querySelector(".d").textContent = String(detail);
    host.appendChild(row);
    while (host.children.length > 8) host.removeChild(host.firstChild);
  };
  ws.onclose = () => setTimeout(connectStudioWS, 4000);
}

load();
loadPlayground();
