// Agent Studio — consola de configuración del asesor (fases 1 y 2 del plan
// 10_PLAN_AGENT_STUDIO.md).
//
// Principio del PRD: aquí no se escriben prompts. Se mueven controles de un
// vocabulario cerrado y el backend compone las instrucciones. Por eso esta
// pantalla NO tiene ningún campo de texto libre salvo el nombre del agente, y
// las frases que se muestran bajo cada control vienen del backend: si se
// escribieran aquí, un día dirían una cosa y el modelo recibiría otra.
const STUDIO_UI_VERSION = "1.0.0";
console.log(`[guardian-ai] studio UI v${STUDIO_UI_VERSION}`);

const $ = (id) => document.getElementById(id);

let cfg = null;        // borrador en edición (local)
let catalogs = null;
let published = null;
let dirty = false;

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
  renderRuntime(data.runtime);
  renderStates(data.runtime.states);
  renderAll();
  refreshPrompt();
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
  $("statusChip").textContent = dirty ? "borrador sin guardar" : `viva: v${published.version}`;

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
      markDirty();
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
    b.addEventListener("click", () => { cfg.persona.length = len; renderLengths(); markDirty(); });
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
      cfg.sales.goals.splice(index, 1); renderGoals(); markDirty();
    }));
  } else {
    moves.appendChild(moveBtn("Añadir", "Añadir objetivo", false, () => {
      cfg.sales.goals.push(goal); renderGoals(); markDirty();
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
  markDirty();
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
      markDirty();
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
    b.addEventListener("click", () => { cfg.safety.level = level; renderLevels(); markDirty(); });
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
  markDirty();
});

function markDirty() {
  dirty = true;
  $("statusChip").textContent = "borrador sin guardar";
  $("savedNote").hidden = true;
}

function showFieldErrors(errors) {
  $("agentNameErr").hidden = true;
  $("goalsErr").hidden = true;
  for (const err of errors) {
    if (err.field === "persona.agent_name") {
      $("agentNameErr").textContent = err.message;
      $("agentNameErr").hidden = false;
    } else if (err.field === "sales.goals") {
      $("goalsErr").textContent = err.message;
      $("goalsErr").hidden = false;
    } else {
      $("statusChip").textContent = `${err.field}: ${err.message}`;
    }
  }
}

$("saveBtn").addEventListener("click", async () => {
  $("saveBtn").disabled = true;
  try {
    const r = await fetch("/api/studio/config/draft", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(cfg),
    });
    if (r.status === 422) {
      const { errors } = await r.json();
      showFieldErrors(errors || []);
      return;
    }
    if (!r.ok) { $("statusChip").textContent = "no se pudo guardar"; return; }
    const { draft } = await r.json();
    cfg = draft;
    dirty = false;
    showFieldErrors([]);
    renderAll();
    $("savedNote").hidden = false;
    refreshPrompt();
  } finally {
    $("saveBtn").disabled = false;
  }
});

$("resetBtn").addEventListener("click", async () => {
  const r = await fetch("/api/studio/config");
  const data = await r.json();
  cfg = data.defaults;
  cfg.status = "draft";
  markDirty();
  renderAll();
});

load();
