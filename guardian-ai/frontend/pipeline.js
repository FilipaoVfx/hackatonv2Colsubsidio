// Pipeline de Llamadas — post-call analytics view.
// Read-only over /api/analytics/*. Does not touch the live demo or call flow.
const $ = (id) => document.getElementById(id);

// Fases del documento en la paleta institucional Colsubsidio (ver DESIGN.md).
const PHASE_STYLE = [
  { icon: "👋", bg: "#E8F2FA", fg: "#0067B1", line: "#0067B1" },
  { icon: "🔍", bg: "#E4F1EC", fg: "#0F6B4F", line: "#0F6B4F" },
  { icon: "⚠️", bg: "#FBE9F0", fg: "#C53465", line: "#C53465" },
  { icon: "📋", bg: "#FFF7CC", fg: "#8A6D00", line: "#FFD000" },
  { icon: "🤝", bg: "#DCE9F3", fg: "#004A80", line: "#004A80" },
];
const OUTCOME_CLASS = { "Interesado": "", "Cerrado": "", "Seguimiento": "warn", "No interesado": "bad" };

let calls = [], current = null, currentIdx = -1, selectedPhase = 0;

// Etiqueta legible del canal. WhatsApp es el canal de texto; el resto conserva
// la semántica entrante/saliente previa.
const channelLabel = (ch, long) => ch === "whatsapp"
  ? (long ? "WhatsApp 💬" : "WhatsApp")
  : (ch === "inbound" ? (long ? "Llamada Entrante" : "Entrante") : (long ? "Llamada Saliente" : "Saliente"));

const mmss = (s) => `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
const initials = (n) => n.split(" ").filter(Boolean).slice(0, 2).map((w) => w[0]).join("").toUpperCase();
const fmtDate = (iso) => {
  const d = new Date(iso);
  return d.toLocaleDateString("es-CO", { day: "2-digit", month: "short", year: "numeric" }) +
    " - " + d.toLocaleTimeString("es-CO", { hour: "2-digit", minute: "2-digit" });
};

// ---------- list ----------
async function loadList() {
  const r = await fetch("/api/analytics/calls");
  if (!r.ok) { $("callsBody").innerHTML = `<tr><td colspan="7" class="muted">Analítica no disponible (requiere Supabase).</td></tr>`; return; }
  calls = await r.json();
  $("callsBody").innerHTML = "";
  calls.forEach((c, i) => {
    const tr = document.createElement("tr");
    tr.tabIndex = 0; tr.setAttribute("role", "button");
    const cls = c.score_overall >= 85 ? "s-hi" : c.score_overall >= 70 ? "s-mid" : "s-low";
    tr.innerHTML = `<td class="serie-cell">${c.call_code}</td>
      <td>${c.customer}<div class="muted" style="font-size:11px">${c.phone}</div></td>
      <td>${fmtDate(c.started_at)}</td>
      <td>${mmss(c.duration_sec)}</td>
      <td>${channelLabel(c.channel, false)}</td>
      <td><span class="badge-outcome ${OUTCOME_CLASS[c.outcome] ?? "info"}">${c.outcome}</span>
        ${c.lead_state ? `<div class="muted" style="font-size:11px;margin-top:2px">${c.lead_state}</div>` : ""}</td>
      <td class="score-pill ${cls}">${c.score_overall}
        ${c.cost_usd ? `<div class="muted" style="font-size:11px">$${c.cost_usd.toFixed(4)} · ${(c.tokens_in || 0) + (c.tokens_out || 0)} tok</div>` : ""}</td>`;
    tr.addEventListener("click", () => openDetail(i));
    tr.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openDetail(i); } });
    $("callsBody").appendChild(tr);
  });
  if (calls.length) $("headDate").textContent = fmtDate(calls[0].started_at);

  // deep link: /pipeline#<call_id> or #<call_code> opens that call directly
  const h = decodeURIComponent(location.hash.slice(1));
  if (h) {
    const i = calls.findIndex((c) => c.call_id === h || c.call_code === h);
    if (i >= 0) openDetail(i);
  }
}

// ---------- detail ----------
async function openDetail(idx) {
  currentIdx = idx;
  const r = await fetch(`/api/analytics/calls/${calls[idx].call_id}`);
  if (!r.ok) { alert("No se pudo cargar el detalle"); return; }
  current = await r.json();
  selectedPhase = 0;
  renderDetail();
  $("listView").hidden = true;
  $("detailView").hidden = false;
  window.scrollTo(0, 0);
}

function renderDetail() {
  const d = current, c = d.customer;
  $("chAvatar").textContent = initials(c.full_name);
  $("chName").textContent = c.full_name;
  $("chPhone").textContent = c.phone;
  $("chNew").style.display = c.is_new ? "" : "none";
  $("chCode").textContent = d.call_code;
  $("chDate").textContent = fmtDate(d.started_at);
  $("chDur").textContent = mmss(d.duration_sec) + " min";
  $("chAdvisor").textContent = `${d.advisor_name} (${d.advisor_voice})`;
  $("chChannel").textContent = channelLabel(d.channel, true);
  const oc = $("chOutcome");
  oc.textContent = d.outcome;
  oc.className = "badge-outcome " + (OUTCOME_CLASS[d.outcome] ?? "info");
  $("headDate").textContent = fmtDate(d.started_at);
  $("pTotal").textContent = mmss(d.duration_sec);
  loadAudio(d.recording_url);

  renderPhases();
  renderTranscript();
  renderScore();
  renderProfile();
  renderInsights();
  selectPhase(0);
}

function renderPhases() {
  const box = $("phases"); box.innerHTML = "";
  current.phases.forEach((p, i) => {
    const st = PHASE_STYLE[i % 5];
    const el = document.createElement("div");
    el.className = "phase" + (i === selectedPhase ? " active" : "");
    el.innerHTML = `<div class="ph-top">
        <span class="ph-icon" style="background:${st.bg};color:${st.fg}">${st.icon}</span>
        <span class="ph-name">${p.idx}. ${p.name}</span></div>
      <div class="ph-time">${mmss(p.start_sec)} - ${mmss(p.end_sec)}
        <span class="ph-pct">${p.pct_of_total}%</span></div>`;
    el.tabIndex = 0; el.setAttribute("role", "button");
    el.addEventListener("click", () => selectPhase(i));
    el.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); selectPhase(i); } });
    box.appendChild(el);
  });

  // timeline segments + nodes
  const track = $("tTrack"); track.innerHTML = "";
  const total = current.duration_sec || 1;
  current.phases.forEach((p, i) => {
    const st = PHASE_STYLE[i % 5];
    const seg = document.createElement("div");
    seg.className = "t-seg";
    seg.style.left = (p.start_sec / total * 100) + "%";
    seg.style.width = ((p.end_sec - p.start_sec) / total * 100) + "%";
    seg.style.background = st.line;
    track.appendChild(seg);
    const node = document.createElement("div");
    node.className = "t-node";
    node.style.left = (p.end_sec / total * 100) + "%";
    node.style.background = st.line;
    track.appendChild(node);
  });
  $("tEnd").textContent = mmss(current.duration_sec);
}

function selectPhase(i) {
  selectedPhase = i;
  document.querySelectorAll(".phase").forEach((el, j) => el.classList.toggle("active", j === i));
  const p = current.phases[i];
  if (!p) return;
  $("psName").textContent = `${p.idx}. ${p.name}`;
  $("psStatus").textContent = p.status || "Completada";
  $("psDur").innerHTML = `${mmss(p.start_sec)} - ${mmss(p.end_sec)}<div class="muted" style="font-size:11px">(${p.end_sec - p.start_sec} seg)</div>`;
  $("psAgent").textContent = p.agent_pct + "%";
  $("psCustomer").textContent = p.customer_pct + "%";
  $("psObjective").textContent = p.objective || "—";
  $("psChecklist").innerHTML = (p.checklist || []).map((t) => `<li>${t}</li>`).join("");
  $("psKeywords").innerHTML = (p.keywords || []).map((k) => `<span class="kw">${k}</span>`).join("");
  const emo = (p.emotion || "").toLowerCase();
  $("psEmoji").textContent = emo === "positiva" ? "🙂" : emo === "negativa" ? "🙁" : "😐";
  $("psEmotion").textContent = p.emotion || "—";
  $("psEmotion").className = "e-name" + (emo === "negativa" ? " neg" : emo === "neutral" ? " neu" : "");
  $("psConf").textContent = "Confianza: " + Math.round((p.emotion_conf || 0) * 100) + "%";
  $("psFindings").textContent = p.findings || "—";
}

function renderTranscript() {
  const q = ($("tSearch").value || "").toLowerCase();
  const role = $("tSpeaker").value;
  const box = $("transcript"); box.innerHTML = "";
  current.transcript
    .filter((u) => (!role || u.role === role) && (!q || u.text.toLowerCase().includes(q)))
    .forEach((u) => {
      const el = document.createElement("div");
      el.className = "utt " + u.role;
      el.innerHTML = `<div class="utt-head">
          <span class="utt-who"><span class="ico">${u.role === "agent" ? "▶" : initials(u.speaker)}</span>${u.speaker}</span>
          <span class="utt-time">${mmss(u.at_sec)}</span></div>
        <div>${u.text}</div>`;
      box.appendChild(el);
    });
  if (!box.children.length) box.innerHTML = `<div class="muted">Sin resultados.</div>`;
}

function renderScore() {
  const s = current.score_overall;
  $("scoreVal").textContent = s;
  $("scoreLabel").textContent = current.score_label;
  // sello de calificación: la tinta cambia con el puntaje (verde / ámbar / rojo)
  $("scoreSeal").className = "gauge" + (s >= 85 ? "" : s >= 70 ? " mid" : " low");
  $("dims").innerHTML = current.scores.map((d) => `
    <div class="dim"><div class="dim-top"><span>${d.label}</span><strong>${d.value}</strong></div>
      <div class="dim-bar"><div class="dim-fill" style="width:${d.value}%;
        background:${d.value >= 85 ? "var(--seal-ok)" : d.value >= 70 ? "var(--amber-ink)" : "var(--alert)"}"></div></div></div>`).join("");
}

function renderProfile() {
  const c = current.customer;
  const rows = [
    ["👤", c.full_name],
    ["🎂", c.age + " años"],
    ["💍", c.marital_status + (c.children ? `, ${c.children} hijo${c.children > 1 ? "s" : ""}` : "")],
    ["💼", c.occupation],
    ["💵", "Ingreso mensual: " + c.income_range],
    ["📍", "Vive en " + c.city],
  ];
  $("profile").innerHTML = rows.map(([i, t]) => `<li><span class="pi">${i}</span><span>${t}</span></li>`).join("");
}

function renderInsights() {
  $("insights").innerHTML = current.insights.map((i) => {
    const ico = { riesgo: "⚠️", precio: "💵", motivacion: "❤️", producto: "📦",
                  seguimiento: "📅", oportunidad: "⭐", competencia: "🏷️",
                  crosssell: "🔗", coaching: "🎓" }[i.category] || "💡";
    return `<li><span>${ico}</span><span>${i.text}</span><span class="pri ${i.priority}">${i.priority}</span></li>`;
  }).join("");
}

// ---------- events ----------
$("backBtn").addEventListener("click", () => {
  $("detailView").hidden = true; $("listView").hidden = false;
});
$("prevBtn").addEventListener("click", () => { if (currentIdx > 0) openDetail(currentIdx - 1); });
$("nextBtn").addEventListener("click", () => { if (currentIdx < calls.length - 1) openDetail(currentIdx + 1); });
$("tSearch").addEventListener("input", () => current && renderTranscript());
$("tSpeaker").addEventListener("change", () => current && renderTranscript());
$("exportBtn").addEventListener("click", () => {
  if (!current) { alert("Abre una llamada para exportar."); return; }
  const blob = new Blob([JSON.stringify(current, null, 2)], { type: "application/json" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = current.call_code + ".json";
  a.click();
});
$("tabs").addEventListener("click", (e) => {
  if (!e.target.classList.contains("tab")) return;
  document.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
  e.target.classList.add("active");
});

// ---------- reproductor de la grabación ----------
// El audio sintetizado con ElevenLabs es una dramatización condensada de la
// llamada: dura menos que `duration_sec` (la duración nominal registrada).
// La barra se rige por el audio real; el encabezado conserva la nominal.
const RATES = [1, 1.25, 1.5, 0.75];
let rateIdx = 0;

function audioEl() { return $("pAudio"); }

function loadAudio(url) {
  const a = audioEl();
  if (!a) return;
  a.pause();
  a.removeAttribute("src");
  $("pFill").style.width = "0%";
  $("pCur").textContent = "00:00";
  $("pPlay").textContent = "▶";
  if (!url) { $("pPlay").disabled = true; $("pPlay").title = "Sin grabación"; return; }
  $("pPlay").disabled = false;
  $("pPlay").title = "Reproducir";
  a.src = url;
  a.playbackRate = RATES[rateIdx];
  a.load();
}

(function wirePlayer() {
  const a = audioEl();
  if (!a) return;

  a.addEventListener("loadedmetadata", () => {
    if (isFinite(a.duration)) $("pTotal").textContent = mmss(Math.round(a.duration));
  });
  a.addEventListener("timeupdate", () => {
    if (!isFinite(a.duration) || a.duration === 0) return;
    $("pFill").style.width = (a.currentTime / a.duration * 100) + "%";
    $("pCur").textContent = mmss(Math.floor(a.currentTime));
  });
  a.addEventListener("ended", () => { $("pPlay").textContent = "▶"; });
  a.addEventListener("error", () => {
    $("pPlay").disabled = true;
    $("pPlay").title = "Grabación no disponible";
  });

  $("pPlay").addEventListener("click", () => {
    if (!a.src) return;
    if (a.paused) { a.play(); $("pPlay").textContent = "⏸"; }
    else { a.pause(); $("pPlay").textContent = "▶"; }
  });
  $("pBack").addEventListener("click", () => { a.currentTime = Math.max(0, a.currentTime - 10); });
  $("pFwd").addEventListener("click", () => {
    if (isFinite(a.duration)) a.currentTime = Math.min(a.duration, a.currentTime + 10);
  });
  $("pRate").addEventListener("click", () => {
    rateIdx = (rateIdx + 1) % RATES.length;
    a.playbackRate = RATES[rateIdx];
    $("pRate").textContent = RATES[rateIdx].toFixed(2).replace(/0$/, "") + "x";
  });
  $("pBar").addEventListener("click", (e) => {
    if (!isFinite(a.duration)) return;
    const r = $("pBar").getBoundingClientRect();
    a.currentTime = Math.min(a.duration, Math.max(0, (e.clientX - r.left) / r.width * a.duration));
  });
})();

loadList();
