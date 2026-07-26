package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GuardianEngine — the Guardian Conversation Engine (spec retrieval.md).
// Converts a WhatsApp lead into a "Lead Ready for Advisor": the LLM only
// understands/extracts/explains; every business decision (affiliation,
// eligibility, recommendation) comes from the Colsubsidio Protege API; the
// lead state machine is driven deterministically by this engine.
//
// Channel-independent: it touches only the bus + sessions, never Kapso.
type GuardianEngine struct {
	bus        *EventBus
	api        *ColsubsidioClient
	llm        GuardianLLM
	tools      *Tools
	sessions   *WhatsAppSessions
	rag        *RAG
	affiliates *Affiliates // Afiliado 360: precarga de perfil (nil-safe)

	mu    sync.Mutex
	convs map[string]*guardianConv

	// turns serializa los turnos de un MISMO cliente. WhatsApp entrega en
	// ráfaga y el webhook lanza una goroutine por mensaje: sin esta llave por
	// teléfono dos turnos comparten el mismo guardianConv (historial, estado)
	// y además pueden abrir dos conversaciones para la misma persona.
	turns keyedMutex

	// per-process catalog cache (products/rules change rarely). Solo se cachea
	// el ÉXITO: con sync.Once un primer fallo dejaba al prompt sin catálogo
	// hasta reiniciar el proceso, y el agente sin productos que nombrar.
	catMu    sync.Mutex
	products []ProtegeProduct
	rules    []ProtegeRule

	// cfg es la configuración publicada del Agent Studio. Se lee UNA vez al
	// empezar cada turno y ese snapshot inmutable sirve todo el turno: publicar
	// a mitad de una conversación cambia el puntero, nunca la configuración que
	// el turno en curso ya está usando. Sin locks en la ruta caliente.
	cfg atomic.Pointer[AgentConfig]
}

type guardianConv struct {
	userID    string
	phone     string
	state     LeadState
	history   []oaMessage
	questions []ProtegeQuestion
	recs      []string // rendered recommendations shown in MATCHING
	recTries  int      // intentos de get_recommendations en MATCHING
	stall     int      // turnos seguidos sin descubrir variable nueva

	// Cierre (ver closing.go): las opciones que la API rankeó, la elegida, las
	// coberturas que el cliente añadió y la cotización vigente. La cotización
	// se guarda para que la vinculación se emita contra el precio que el
	// cliente YA vio, nunca contra uno recalculado por detrás.
	options []recOption
	picked  int
	addons  map[string]bool
	quote   *ProtegeQuote
}

func NewGuardianEngine(bus *EventBus, api *ColsubsidioClient, llm GuardianLLM, tools *Tools, sessions *WhatsAppSessions, rag *RAG, affiliates *Affiliates) *GuardianEngine {
	return &GuardianEngine{
		bus: bus, api: api, llm: llm, tools: tools, sessions: sessions, rag: rag,
		affiliates: affiliates,
		convs:      make(map[string]*guardianConv),
	}
}

func (e *GuardianEngine) Enabled() bool {
	return e != nil && e.api != nil && e.api.Enabled() && e.llm != nil
}

// SetConfig publica una configuración del Agent Studio. La copia se guarda
// completa y no vuelve a mutarse: los turnos que ya empezaron terminan con la
// anterior, el siguiente turno de CUALQUIER conversación usa la nueva.
func (e *GuardianEngine) SetConfig(cfg AgentConfig) {
	if e == nil {
		return
	}
	snapshot := cfg.Clone()
	e.cfg.Store(&snapshot)
}

// Config devuelve el snapshot vivo. Nunca nil: sin configuración publicada, el
// motor se comporta con los defaults de fábrica.
func (e *GuardianEngine) Config() AgentConfig {
	if e == nil {
		return DefaultConfig()
	}
	if cfg := e.cfg.Load(); cfg != nil {
		return *cfg
	}
	return DefaultConfig()
}

// ragTopK: cuántos fragmentos de documentación entran al prompt cuando el
// cliente pregunta algo informativo. El Agent Studio lo muestra en solo lectura.
const ragTopK = 2

const guardianFallbackMsg = "Estoy validando tu información, dame un momento por favor y seguimos."

// StartContact opens an outbound Guardian conversation. When greet is true a
// static opener is sent (24h-window template rule: the FIRST outbound message
// is fixed, not LLM free-form).
func (e *GuardianEngine) StartContact(ctx context.Context, phone string) (string, error) {
	unlock := e.turns.lock(canonPhone(phone))
	defer unlock()
	return e.start(ctx, phone, true)
}

func (e *GuardianEngine) start(ctx context.Context, phone string, greet bool) (string, error) {
	// AFFILIATION_CHECK is resolved by the API, not the LLM: search first.
	var user *ProtegeUser
	isNew := false
	// callID unknown yet: emit the identification tools on a provisional id is
	// noisy — resolve silently, then emit everything on the real conversation id.
	if u, err := e.api.SearchUserByPhone(ctx, phone); err != nil {
		return "", err
	} else if u != nil {
		user = u
	} else {
		created, err := e.api.CreateUser(ctx, ProtegeUser{Phone: phone})
		if err != nil {
			return "", err
		}
		user, isNew = created, true
	}
	conv, err := e.api.StartConversation(ctx, user.ID, phone)
	if err != nil {
		return "", err
	}
	callID := conv.ID

	e.bus.Publish(callID, CALL_STARTED, "guardian_engine", map[string]interface{}{
		"from": phone, "channel": "whatsapp", "engine": "guardian",
		"user_id": user.ID, "is_new_user": isNew,
	})
	e.transitionRaw(callID, StateNew, StateAffiliation, "conversación abierta")
	// Semántica precisa: esto es el estado en el PIPELINE de ventas (Protege
	// API), no la afiliación a Colsubsidio. Ser "nuevo" aquí y a la vez
	// afiliado conocido del maestro 360 es el caso de negocio normal.
	e.bus.Publish(callID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
		"key": "estado_pipeline", "value": map[bool]string{true: "nuevo", false: "conocido"}[isNew],
		"previous": nil, "source": "colsubsidio_api",
	})
	e.transitionRaw(callID, StateAffiliation, StateProfile, "identidad resuelta por la API")

	questions, qErr := e.fetchQuestions(ctx, callID)
	if qErr != nil || len(questions) == 0 {
		// El descubrimiento depende de este catálogo: se declara el fallo y el
		// turno lo reintenta (nunca se interpreta como "nada que preguntar").
		e.bus.Publish(callID, ERROR_OCCURRED, "guardian_engine", map[string]interface{}{
			"source": "colsubsidio_api", "code": "questions_unavailable",
			"message": "catálogo de preguntas no disponible al abrir la conversación", "recoverable": true,
		})
	}
	e.mu.Lock()
	e.convs[callID] = &guardianConv{userID: user.ID, phone: phone, state: StateProfile, questions: questions}
	e.mu.Unlock()
	e.sessions.Register(phone, callID)

	// Afiliado 360: ESTIMACIÓN inicial del perfil (vinculación demo por hash —
	// la base es anónima, sin teléfonos). fuente_perfil lo declara en la UI.
	// Cuando el cliente confirma su número de afiliado en la conversación,
	// applySerie() lo reemplaza por el registro REAL del maestro.
	if isNew && e.affiliates.Enabled() {
		if af, ok := e.affiliates.ForPhone(phone); ok {
			e.preload(ctx, callID, user.ID, af, "estimación demo (hash de teléfono)", false)
		}
	}

	if greet {
		e.sendAgent(callID, phone, "¡Hola! Soy Guardian, tu asesor de protección de Colsubsidio 🛡️. "+
			"Me encantaría conocerte un poco para recomendarte la protección que de verdad te sirva. ¿Cómo te llamas?")
	}
	return callID, nil
}

// HandleInbound processes one inbound WhatsApp message through a full Guardian
// turn. A message from an unknown phone opens the conversation first (the
// customer wrote first: their text IS the first turn, no canned greeting).
func (e *GuardianEngine) HandleInbound(ctx context.Context, phone, text string) (string, error) {
	// Un turno a la vez por cliente: resolver-o-abrir la sesión y ejecutar el
	// turno son atómicos frente a otro mensaje del mismo teléfono.
	unlock := e.turns.lock(canonPhone(phone))
	defer unlock()

	convID, isNew := e.sessions.Resolve(phone)
	if isNew {
		id, err := e.start(ctx, phone, false)
		if err != nil {
			return "", err
		}
		convID = id
	}
	return convID, e.turn(ctx, convID, phone, text)
}

// turn is one full Guardian turn (spec §4 flow). Never panics the webhook.
func (e *GuardianEngine) turn(ctx context.Context, convID, phone, text string) error {
	started := time.Now()
	e.mu.Lock()
	st := e.convs[convID]
	e.mu.Unlock()
	if st == nil {
		return fmt.Errorf("guardian: unknown conversation %s", convID)
	}

	// Snapshot de configuración del turno: se lee UNA vez y no se vuelve a
	// consultar. Si alguien publica en el Studio mientras este turno corre, el
	// cambio entra en el siguiente — nunca a mitad de una respuesta.
	cfg := e.Config()

	e.bus.Publish(convID, MESSAGE_RECEIVED, "whatsapp_adapter", map[string]interface{}{"is_final": true})
	e.bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
		"role": "user", "text": text, "is_final": true,
	})

	var toolCalls []string
	runTool := func(name string, args map[string]interface{}) ToolResult {
		toolCalls = append(toolCalls, name)
		return e.tools.Run(ctx, convID, name, args)
	}

	// 0. El catálogo de preguntas pudo fallar al abrir (API intermitente). Sin
	// él la etapa nunca se completa, así que se reintenta cada turno.
	if len(st.questions) == 0 {
		if qs, err := e.fetchQuestions(ctx, convID); err == nil && len(qs) > 0 {
			st.questions = qs
		}
	}

	// 1. Strategic memory — ALWAYS rebuilt from the API (spec §6).
	memory := CustomerMemory{User: &ProtegeUser{ID: st.userID, Phone: st.phone}}
	if res := runTool("get_variables", map[string]interface{}{"user_id": st.userID}); res.Err == nil {
		if vars, ok := res.Data.([]UserVariable); ok {
			memory.Variables = vars
		}
	}
	known := memory.Known()

	// 2. Catalog (once per process) + retrieved docs (heuristic: questions).
	products, rules := e.catalog(ctx, convID)
	var retrieved []Chunk
	if e.rag.Enabled() && looksLikeQuestion(text) {
		retrieved = e.rag.Retrieve(ctx, text, ragTopK)
		if len(retrieved) > 0 {
			refs := make([]map[string]string, len(retrieved))
			for i, c := range retrieved {
				refs[i] = map[string]string{"doc": c.Doc, "heading": c.Heading}
			}
			e.bus.Publish(convID, KNOWLEDGE_RETRIEVED, "guardian_engine", map[string]interface{}{
				"query": text, "chunks": refs, "mode": e.rag.Mode(),
			})
		}
	}

	// 3. Modular prompt + structured LLM turn.
	prompt := BuildSystemPrompt(PromptInput{
		Config: cfg,
		State:  st.state, Memory: memory, Products: products, Rules: rules,
		MissingVars: MissingQuestions(st.state, st.questions, known),
		Retrieved:   retrieved, Recs: st.recs,
	})
	st.history = trimHistory(append(st.history, oaMessage{Role: "user", Content: text}))

	e.bus.Publish(convID, LLM_REQUESTED, "guardian_engine", map[string]interface{}{
		"strategy": string(st.state), "config_version": cfg.Version,
	})
	d, err := e.llm.DecideGuardian(ctx, prompt, st.history)
	if err != nil {
		e.bus.Publish(convID, ERROR_OCCURRED, "llm_gateway", map[string]interface{}{
			"source": "llm_gateway", "code": "llm_error", "message": err.Error(), "recoverable": true,
		})
		e.sendAgent(convID, phone, guardianFallbackMsg)
		e.turnCompleted(convID, st, started, turnOutcome{
			toolCalls: toolCalls, configVersion: cfg.Version, err: err,
		})
		return nil // webhook stays 200; error already registered
	}
	e.bus.Publish(convID, LLM_RESPONSE, "llm_gateway", map[string]interface{}{
		"text": d.AssistantMessage, "tool_calls": []interface{}{},
		"tokens_in": d.TokensIn, "tokens_out": d.TokensOut,
		"cost_usd": d.CostUSD, "latency_ms": d.LatencyMS, "model": model,
		"strategy": string(st.state),
	})
	if d.Intent != "" {
		e.bus.Publish(convID, INTENT_DETECTED, "guardian_engine", map[string]interface{}{
			"intent": d.Intent, "confidence": d.Confidence,
		})
	}

	// 4. Immediate persistence of confirmed facts (spec §4 fase 3).
	newVars, rejectedVars := e.persistEntities(ctx, convID, st, d.Entities, known, runTool)
	// Antiatasco: un turno que no descubre nada cuenta; con maxStallTurns
	// seguidos, maybeAdvance camina la flecha igual (ver maxStallTurns).
	st.noteProgress(len(newVars))
	// Cliente confirmó su número de afiliado → lookup REAL en el maestro 360.
	e.applySerie(ctx, convID, st, d.Entities)

	// 5. Deterministic state advancement (the LLM proposes; the engine decides).
	// La acción se valida contra la whitelist del estado y TODO lo que sigue usa
	// la acción validada: leer d.NextAction crudo más abajo era saltarse la
	// whitelist que acabamos de aplicar.
	action := d.NextAction
	if !ActionAllowed(st.state, action) {
		action = FallbackAction(st.state)
		e.bus.Publish(convID, ERROR_OCCURRED, "guardian_engine", map[string]interface{}{
			"source": "guardian_engine", "code": "illegal_action",
			"message":     fmt.Sprintf("next_action %q no permitida en %s, degradada a %q", d.NextAction, st.state, action),
			"recoverable": true,
		})
	}
	reply := strings.TrimSpace(d.AssistantMessage)
	if reply == "" {
		reply = guardianFallbackMsg
	}
	// UX: si la siguiente variable por descubrir es simple (boolean o select de
	// ≤3 opciones) y el turno es una pregunta, adjunta botones quick-reply de
	// WhatsApp — un tap responde y vuelve como texto por el mismo pipeline.
	var buttons []string
	if missing := MissingQuestions(st.state, st.questions, known); len(missing) > 0 &&
		strings.Contains(reply, "?") {
		buttons = buttonsForQuestion(&missing[0])
	}
	e.sendAgentWithButtons(convID, phone, reply, buttons)

	// El handoff dejó de ser el cierre por defecto: solo escala la ACCIÓN
	// explícita. El intent no basta — en pruebas reales el modelo etiquetó
	// "cuéntame cuál me recomiendas" como request_advisor y eso terminaba la
	// conversación derivando a un humano que nadie había pedido.
	wantsAdvisor := action == ActionHandoff
	// La aceptación mueve el cierre estando en MATCHING/CLOSING; en etapas
	// anteriores un "sí" es solo continuidad de la conversación.
	accepts := (action == ActionAccept || isAcceptIntent(d.Intent)) &&
		(st.state == StateMatching || st.state == StateClosing)
	switch {
	case action == ActionClose && closeCorroborated(st.state, d.Intent):
		e.finishNurturing(ctx, convID, phone, "el cliente cerró la conversación")
	case wantsAdvisor:
		// Pidió un humano: se camina hasta READY por flechas LEGALES, sin
		// inventar una recomendación con un perfil a medias.
		e.escalate(ctx, convID, phone, st, known, runTool)
	case accepts:
		e.advanceClosing(ctx, convID, phone, st, known, text, runTool)
	case action == ActionAdjust && (st.state == StateMatching || st.state == StateClosing):
		e.adjustOffer(ctx, convID, phone, st, text)
	case action == ActionRecommend:
		e.fastForward(ctx, convID, phone, st, known, runTool)
	case st.state == StateMatching && len(st.recs) == 0:
		// El intento anterior de recomendar falló: se reintenta en vez de
		// quedarse mudo en MATCHING para siempre.
		e.enterMatching(ctx, convID, phone, st, runTool)
	default:
		e.maybeAdvance(ctx, convID, phone, st, known, runTool)
	}

	e.turnCompleted(convID, st, started, turnOutcome{
		intent: d.Intent, confidence: d.Confidence,
		newVars: newVars, rejectedVars: rejectedVars,
		toolCalls: toolCalls, configVersion: cfg.Version,
	})
	return nil
}

// preload saves an affiliate profile into the API and emits the features.
// fuente declara el origen en la UI: "estimación demo (hash)" al abrir, o
// "maestro de afiliados (serie confirmada)" cuando el cliente da su número.
//
// confirmed distingue las dos: la vinculación por hash es una ESTIMACIÓN de
// demo (la base es anónima, sin teléfonos), así que entra con confianza baja y
// sin `monthly_income` — ese dato calificaría la etapa financiera y dispararía
// reglas de capacidad con un ingreso que nadie confirmó. Con la serie que da el
// cliente sí es el registro real del maestro y entra completo.
func (e *GuardianEngine) preload(ctx context.Context, callID, userID string, af Affiliate, fuente string, confirmed bool) {
	conf := 1.0
	vars := append(af.Variables(confirmed),
		VariableValue{Key: "fuente_perfil", Value: fuente, Source: "colsubsidio_360", Confidence: &conf})
	if res := e.tools.Run(ctx, callID, "save_variable",
		map[string]interface{}{"user_id": userID, "variables": vars}); res.Err != nil {
		return
	}
	for _, v := range vars {
		e.bus.Publish(callID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
			"key": v.Key, "value": v.Value, "previous": nil, "source": "colsubsidio_360",
		})
	}
}

// entityConfidence es el umbral a partir del cual un hecho extraído por el LLM
// se considera CONFIRMADO y se persiste en el perfil. No es configurable desde
// el Agent Studio: bajarlo llenaría la memoria estratégica de suposiciones.
const entityConfidence = 0.6

// serieKeys son las claves de entity con las que el LLM reporta el número de
// afiliado/cédula que el cliente comparte en la conversación.
var serieKeys = map[string]bool{"affiliate_serie": true, "numero_afiliado": true, "cedula": true, "document_number": true}

// applySerie hace el lookup REAL en el maestro cuando el cliente confirma su
// número de afiliado; reemplaza la estimación inicial (upsert de variables).
func (e *GuardianEngine) applySerie(ctx context.Context, callID string, st *guardianConv, entities []GuardianEntity) {
	if !e.affiliates.Enabled() {
		return
	}
	for _, ent := range entities {
		if !serieKeys[strings.ToLower(ent.Key)] || ent.Confidence < entityConfidence {
			continue
		}
		if af, ok := e.affiliates.BySerie(fmt.Sprint(ent.Value)); ok {
			e.preload(ctx, callID, st.userID, af, "maestro de afiliados (serie confirmada)", true)
			return
		}
	}
}

// acceptedKeys is the closed vocabulary the LLM may write into the customer's
// profile: las variable_key del catálogo de la API, más las claves con las que
// el cliente comparte su número de afiliado. Todo lo demás es una clave
// inventada por el modelo y NO entra a la memoria estratégica.
//
// Las claves ya presentes en la API se aceptan aunque no estén en el catálogo:
// las escribió el motor (perfil 360, fuente_perfil) o un catálogo anterior.
func acceptedKeys(questions []ProtegeQuestion, known map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(questions)+len(known)+len(serieKeys))
	for _, q := range questions {
		if q.VariableKey != "" {
			out[strings.ToLower(q.VariableKey)] = true
		}
	}
	for k := range known {
		out[strings.ToLower(k)] = true
	}
	for k := range serieKeys {
		out[k] = true
	}
	return out
}

// persistEntities saves confident extracted facts IMMEDIATELY and returns the
// keys written. Las claves fuera del vocabulario se descartan y se reportan en
// TURN_COMPLETED.rejected_variables (trazabilidad sin ruido de errores).
func (e *GuardianEngine) persistEntities(ctx context.Context, convID string, st *guardianConv,
	entities []GuardianEntity, known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) (saved, rejected []string) {

	accepted := acceptedKeys(st.questions, known)
	var batch []VariableValue
	var keys []string
	for _, ent := range entities {
		if ent.Key == "" || ent.Confidence < entityConfidence {
			continue
		}
		if !accepted[strings.ToLower(ent.Key)] {
			rejected = append(rejected, ent.Key)
			continue
		}
		conf := ent.Confidence
		batch = append(batch, VariableValue{Key: ent.Key, Value: ent.Value, Source: "whatsapp", Confidence: &conf})
		keys = append(keys, ent.Key)
	}
	if len(batch) == 0 {
		return nil, rejected
	}
	res := runTool("save_variable", map[string]interface{}{"user_id": st.userID, "variables": batch})
	if res.Err != nil {
		return nil, rejected // ERROR_OCCURRED ya emitido por el tool engine si aplica
	}
	for _, v := range batch {
		prev := known[v.Key]
		known[v.Key] = v.Value
		e.bus.Publish(convID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
			"key": v.Key, "value": v.Value, "previous": prev, "source": "whatsapp",
		})
	}
	return keys, rejected
}

// minProfileVars es el mínimo de variables del catálogo con las que se acepta
// recomendar cuando el cliente lo pide sin haber contado todo. Por debajo de
// eso no es acelerar el recorrido, es inventarse el match.
const minProfileVars = 3

// maxStallTurns: turnos seguidos en la MISMA etapa de descubrimiento sin
// descubrir ni una variable nueva. Al superarlo el motor avanza con lo que
// tiene. Sin este tope, una variable que el modelo no logra extraer (pasó con
// `num_dependents` cuando el cliente dice "dos hijos") deja la conversación
// preguntando lo mismo para siempre y la venta no se cierra nunca.
const maxStallTurns = 3

// knownProfileVars cuenta cuántas variables del catálogo de la API ya se
// descubrieron (las del perfil, no las financieras).
func knownProfileVars(questions []ProtegeQuestion, known map[string]interface{}) int {
	n := 0
	for _, q := range questions {
		if isFinancialVar(q.VariableKey) {
			continue
		}
		if _, have := known[q.VariableKey]; have {
			n++
		}
	}
	return n
}

// noteProgress lleva la cuenta de turnos sin descubrimiento en la etapa actual
// y reporta si la conversación está estancada.
func (st *guardianConv) noteProgress(newVars int) bool {
	if st.state != StateProfile && st.state != StateFinancial {
		st.stall = 0
		return false
	}
	if newVars > 0 {
		st.stall = 0
		return false
	}
	st.stall++
	return st.stall >= maxStallTurns
}

// maybeAdvance walks ONE legal arrow when the current stage is complete, o
// cuando la etapa se atascó (ver maxStallTurns).
func (e *GuardianEngine) maybeAdvance(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if !StageComplete(st.state, st.questions, known) {
		if st.stall < maxStallTurns {
			return
		}
		// Atascado: se avanza con lo descubierto y queda registrado por qué.
		st.stall = 0
		switch st.state {
		case StateProfile:
			e.transition(convID, st, StateFinancial, fmt.Sprintf("sin descubrimiento nuevo en %d turnos", maxStallTurns))
		case StateFinancial:
			e.transition(convID, st, StateMatching, fmt.Sprintf("sin descubrimiento nuevo en %d turnos", maxStallTurns))
			e.enterMatching(ctx, convID, phone, st, runTool)
		}
		return
	}
	switch st.state {
	case StateProfile:
		e.transition(convID, st, StateFinancial, "perfil completo")
	case StateFinancial:
		e.transition(convID, st, StateMatching, "calificación financiera completa")
		e.enterMatching(ctx, convID, phone, st, runTool)
	}
}

// fastForward honors an explicit customer request for a recommendation by
// walking the LEGAL arrows to matching (no skipped states, spec §3.3).
//
// Ya no exige la etapa COMPLETA: pedir la recomendación y que el agente siga
// preguntando es la forma más rápida de perder la venta. Basta un perfil
// mínimo (minProfileVars); con menos que eso se sigue descubriendo, porque
// recomendar sobre un perfil vacío sí sería inventarse el match.
func (e *GuardianEngine) fastForward(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if st.state == StateProfile && !StageComplete(StateProfile, st.questions, known) &&
		knownProfileVars(st.questions, known) < minProfileVars {
		e.maybeAdvance(ctx, convID, phone, st, known, runTool)
		return
	}
	if st.state == StateProfile {
		e.transition(convID, st, StateFinancial, "cliente pidió recomendación")
	}
	if st.state == StateFinancial {
		e.transition(convID, st, StateMatching, "cliente pidió recomendación")
		e.enterMatching(ctx, convID, phone, st, runTool)
	}
}

// escalate honors an explicit request for a human advisor: walks the LEGAL
// arrows up to PROJECT_MATCHING and closes as READY_FOR_ADVISOR. No genera
// recomendaciones: si el perfil está a medias, el asesor lo completa — mejor
// eso que un match fabricado sobre datos que nadie confirmó.
func (e *GuardianEngine) escalate(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if st.state == StateProfile {
		e.transition(convID, st, StateFinancial, "el cliente pidió un asesor")
	}
	if st.state == StateFinancial {
		e.transition(convID, st, StateMatching, "el cliente pidió un asesor")
	}
	if st.state != StateMatching && st.state != StateClosing {
		return // etapa terminal o no escalable: nada que hacer
	}
	e.finishReady(ctx, convID, phone, st, known, runTool)
}

// maxRecAttempts limita los reintentos de recomendación antes de derivar el
// lead: sin tope, un motor caído dejaba la conversación viva pero muda.
const maxRecAttempts = 3

// enterMatching asks the API for recommendations (the API decides), emits them
// and sends a second LLM-free summary if the LLM cannot be consulted again.
// Reintentable: el turno siguiente vuelve a entrar mientras no haya recs.
func (e *GuardianEngine) enterMatching(ctx context.Context, convID, phone string, st *guardianConv,
	runTool func(string, map[string]interface{}) ToolResult) {

	st.recTries++
	res := runTool("get_recommendations", map[string]interface{}{"user_id": st.userID, "limit": 3})
	if res.Err != nil {
		if st.recTries >= maxRecAttempts {
			e.sendAgent(convID, phone, "No logro generar tu recomendación en este momento. "+
				"Escríbeme de nuevo en un rato y la retomamos desde donde vamos 🙏")
			e.finishNurturing(ctx, convID, phone, "motor de recomendaciones no disponible")
			return
		}
		e.sendAgent(convID, phone, "Estoy generando tu recomendación, dame un momento y seguimos.")
		return
	}
	recs, _ := res.Data.([]interface{})
	st.recs, st.options, st.picked, st.addons, st.quote = nil, nil, -1, nil, nil
	lines := []string{"Con base en lo que me contaste, el sistema me sugiere para ti:"}
	for _, r := range recs {
		opt, ok := parseRecommendation(r)
		if !ok {
			continue
		}
		e.bus.Publish(convID, RECOMMENDATION_GENERATED, "colsubsidio_api", map[string]interface{}{
			"product_name": opt.Name, "reasoning": opt.Reason, "product_id": opt.ProductID, "confidence": 0,
		})
		st.options = append(st.options, opt)
		st.recs = append(st.recs, opt.Line())
		entry := "• " + opt.Line()
		if opt.BasePrice > 0 {
			entry += fmt.Sprintf(" (desde %s/mes)", money(opt.BasePrice))
		}
		lines = append(lines, entry)
	}
	if len(st.recs) == 0 {
		e.sendAgent(convID, phone, "Con tu perfil aún no tengo una recomendación clara. Cuéntame un poco más y lo intentamos de nuevo.")
		e.finishNurturing(ctx, convID, phone, "sin recomendaciones para el perfil")
		return
	}
	// El cierre lo hace el agente, no un asesor: se invita a comparar, ajustar
	// o contratar aquí mismo.
	lines = append(lines, "", "Puedo compararlas, ajustar coberturas o dejarte asegurado hoy mismo. ¿Cuál te sirve más? 😊")
	e.sendAgent(convID, phone, strings.Join(lines, "\n"))
}

// closeCorroborated exige que, en la recta final, la ACCIÓN de cerrar venga
// respaldada por la INTENCIÓN. Cerrar es irreversible (termina la conversación)
// y en pruebas reales el modelo etiquetó "me sirve ese, súmale la cobertura X"
// como close: un cliente que estaba comprando se quedaba sin comprar. Fuera de
// MATCHING/CLOSING se respeta la acción tal cual: ahí cerrar no rompe nada.
func closeCorroborated(state LeadState, intent string) bool {
	if state != StateMatching && state != StateClosing {
		return true
	}
	return intent == "goodbye" || intent == "reject"
}

// currentOption devuelve la opción elegida (por defecto la de mayor score).
func (st *guardianConv) currentOption() (recOption, bool) {
	if st.picked >= 0 && st.picked < len(st.options) {
		return st.options[st.picked], true
	}
	if len(st.options) > 0 {
		return st.options[0], true
	}
	return recOption{}, false
}

// addonKeys son las coberturas opcionales añadidas, en el orden del catálogo.
func (st *guardianConv) addonKeys(opt recOption) []string {
	var out []string
	for _, c := range opt.Optional() {
		if st.addons[c.Key] {
			out = append(out, c.Key)
		}
	}
	return out
}

// adjustOffer atiende "quiero la otra" / "añádeme X": vuelve a cotizar con la
// elección y las coberturas que el cliente nombró. La selección la hace el
// MOTOR sobre el catálogo de la API (closing.go), no el LLM.
func (e *GuardianEngine) adjustOffer(ctx context.Context, convID, phone string, st *guardianConv, text string) {
	if len(st.options) == 0 {
		return
	}
	// "Compárame las dos" no es elegir: responder con la cotización de la
	// primera era contestar otra pregunta. Se comparan y se sigue en MATCHING.
	if wantsComparison(text) {
		e.sendAgent(convID, phone, comparisonMessage(st.options))
		return
	}
	if i := pickOption(st.options, text); i >= 0 {
		st.picked = i
	}
	opt, _ := st.currentOption()
	if st.addons == nil {
		st.addons = map[string]bool{}
	}
	for _, k := range pickCoverages(opt, text) {
		st.addons[k] = true
	}
	e.quoteAndAsk(ctx, convID, phone, st, "el cliente ajustó su plan")
}

// advanceClosing es el cierre en dos pasos: la primera aceptación cotiza y pide
// confirmación explícita; la segunda emite la vinculación. Nunca se contrata
// con un solo "sí" ambiguo.
func (e *GuardianEngine) advanceClosing(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, text string, runTool func(string, map[string]interface{}) ToolResult) {

	if len(st.options) == 0 {
		return
	}
	if st.state == StateMatching {
		if i := pickOption(st.options, text); i >= 0 {
			st.picked = i
		}
		if st.addons == nil {
			st.addons = map[string]bool{}
		}
		opt, _ := st.currentOption()
		for _, k := range pickCoverages(opt, text) {
			st.addons[k] = true
		}
		e.transition(convID, st, StateClosing, "el cliente aceptó una recomendación")
		e.quoteAndAsk(ctx, convID, phone, st, "cotización para confirmar")
		return
	}
	// Ya estaba en CLOSING con una cotización mostrada: esto es la confirmación.
	if st.quote == nil {
		e.quoteAndAsk(ctx, convID, phone, st, "no había cotización vigente")
		return
	}
	e.finishEnrollment(ctx, convID, phone, st, known, runTool)
}

// quoteAndAsk cotiza contra la API y muestra el resumen pidiendo confirmación.
// Si la cotización falla, se escala a un asesor: es la ÚNICA vía honesta, pero
// como excepción y no como cierre por defecto.
func (e *GuardianEngine) quoteAndAsk(ctx context.Context, convID, phone string, st *guardianConv, reason string) {
	opt, ok := st.currentOption()
	if !ok || opt.ProductID == "" {
		return
	}
	q, err := e.api.CreateQuote(ctx, st.userID, opt.ProductID, st.addonKeys(opt))
	if err != nil {
		e.bus.Publish(convID, ERROR_OCCURRED, "colsubsidio_api", map[string]interface{}{
			"source": "colsubsidio_api", "code": "quote_failed", "message": err.Error(), "recoverable": true,
		})
		e.sendAgent(convID, phone, "No logro armar tu cotización en este momento. "+
			"Escríbeme en un momento y la armamos; si prefieres no esperar, dime y lo pasa un asesor 🙏")
		e.finishNurturing(ctx, convID, phone, "cotización no disponible")
		return
	}
	st.quote = q
	e.bus.Publish(convID, QUOTE_CREATED, "colsubsidio_api", map[string]interface{}{
		"quote_id": q.ID, "product_id": q.ProductID, "product_name": q.ProductName,
		"base_price": q.BasePrice, "monthly_price": q.MonthlyPrice, "reason": reason,
	})
	e.sendAgent(convID, phone, quoteMessage(q))
}

// finishEnrollment emite la vinculación y cierra la conversación con la persona
// ASEGURADA: radicado, resumen y enlace al último paso de adquisición.
func (e *GuardianEngine) finishEnrollment(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	enr, err := e.api.CreateEnrollment(ctx, st.userID, st.quote.ID)
	if err != nil {
		e.bus.Publish(convID, ERROR_OCCURRED, "colsubsidio_api", map[string]interface{}{
			"source": "colsubsidio_api", "code": "enrollment_failed", "message": err.Error(), "recoverable": true,
		})
		e.sendAgent(convID, phone, "Tuve un problema al emitir tu solicitud. "+
			"No quiero dejarte a medias: un asesor de Colsubsidio la formaliza y te confirma 🙏")
		e.escalate(ctx, convID, phone, st, known, runTool)
		return
	}
	e.bus.Publish(convID, ENROLLMENT_CREATED, "colsubsidio_api", map[string]interface{}{
		"enrollment_id": enr.ID, "application_number": enr.ApplicationNumber,
		"product_id": enr.ProductID, "product_name": enr.ProductName,
		"monthly_price": enr.MonthlyPrice, "status": enr.Status, "next_step_url": enr.NextStepURL,
	})
	runTool("complete_conversation", map[string]interface{}{"conversation_id": convID, "limit": 3})
	e.sendAgent(convID, phone, enrollmentMessage(enr))
	e.bus.Publish(convID, SUMMARY_GENERATED, "guardian_engine", map[string]interface{}{
		"summary": enr.Summary,
	})
	e.transition(convID, st, StateCompleted, "vinculación confirmada")
	e.bus.Publish(convID, CALL_ENDED, "guardian_engine", map[string]interface{}{"reason": "enrolled"})
	e.close(convID)
}

// finishReady closes the flow as READY_FOR_ADVISOR and emits the LEAD_READY
// handoff package (spec §4 fase 6).
func (e *GuardianEngine) finishReady(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	e.transition(convID, st, StateReady, "cliente aceptó / pidió asesor")
	runTool("complete_conversation", map[string]interface{}{"conversation_id": convID, "limit": 3})

	e.bus.Publish(convID, LEAD_READY, "guardian_engine", map[string]interface{}{
		"user_id": st.userID, "phone": phone, "variables": known, "recommendations": st.recs,
		"summary": fmt.Sprintf("Lead perfilado con %d variable(s) y %d recomendación(es); solicita asesor.", len(known), len(st.recs)),
	})
	e.bus.Publish(convID, SUMMARY_GENERATED, "guardian_engine", map[string]interface{}{
		"summary": "Lead listo para asesor: perfil completo, recomendaciones aceptadas.",
	})
	e.sendAgent(convID, phone, "¡Perfecto! Un asesor de Colsubsidio te contactará muy pronto con todo listo. Gracias por tu confianza 🛡️")
	e.transition(convID, st, StateCompleted, "handoff creado")
	e.bus.Publish(convID, CALL_ENDED, "guardian_engine", map[string]interface{}{"reason": "ready_for_advisor"})
	e.close(convID)
}

// finishNurturing closes the flow into NURTURING (honest stub: message + state).
func (e *GuardianEngine) finishNurturing(ctx context.Context, convID, phone, reason string) {
	e.mu.Lock()
	st := e.convs[convID]
	e.mu.Unlock()
	if st == nil {
		return
	}
	if st.state == StateMatching || st.state == StateClosing || st.state == StateReady {
		e.transition(convID, st, StateNurturing, reason)
	} else {
		// legal path: current → ... only NURTURING reachable from MATCHING/READY;
		// from earlier states we record the intent as a state note, not a jump.
		e.bus.Publish(convID, STATE_CHANGED, "guardian_engine", map[string]interface{}{
			"from": string(st.state), "to": string(st.state), "reason": "cierre anticipado: " + reason,
		})
	}
	e.sendAgent(convID, phone, "¡Gracias por tu tiempo! Te compartiré información útil de vez en cuando y, cuando quieras retomar tu protección, la cerramos aquí mismo 🌟")
	if st.state == StateNurturing {
		e.transition(convID, st, StateCompleted, "nutrición programada")
	}
	e.bus.Publish(convID, SUMMARY_GENERATED, "guardian_engine", map[string]interface{}{
		"summary": "Conversación cerrada hacia nutrición: " + reason,
	})
	e.bus.Publish(convID, CALL_ENDED, "guardian_engine", map[string]interface{}{"reason": "nurturing"})
	e.close(convID)
}

// ---- plumbing ----

func (e *GuardianEngine) transition(convID string, st *guardianConv, to LeadState, reason string) {
	if !CanTransition(st.state, to) {
		e.bus.Publish(convID, ERROR_OCCURRED, "guardian_engine", map[string]interface{}{
			"source": "guardian_engine", "code": "illegal_transition",
			"message": fmt.Sprintf("%s -> %s bloqueada", st.state, to), "recoverable": true,
		})
		return
	}
	from := st.state
	st.state = to
	e.transitionRaw(convID, from, to, reason)
}

func (e *GuardianEngine) transitionRaw(convID string, from, to LeadState, reason string) {
	e.bus.Publish(convID, STATE_CHANGED, "guardian_engine", map[string]interface{}{
		"from": string(from), "to": string(to), "reason": reason,
	})
}

func (e *GuardianEngine) sendAgent(convID, phone, text string) {
	e.sendAgentWithButtons(convID, phone, text, nil)
}

// sendAgentWithButtons igual que sendAgent pero adjunta botones quick-reply en
// el payload de MESSAGE_SENT (el consumer los entrega como mensaje interactivo).
func (e *GuardianEngine) sendAgentWithButtons(convID, phone, text string, buttons []string) {
	e.bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
		"role": "agent", "text": text, "is_final": true,
	})
	payload := map[string]interface{}{
		"text": text, "channel": "whatsapp", "status": "queued", "to": phone, "wa_message_id": "",
	}
	if len(buttons) > 0 {
		payload["buttons"] = buttons
	}
	e.bus.Publish(convID, MESSAGE_SENT, "whatsapp_adapter", payload)
	e.mu.Lock()
	if st := e.convs[convID]; st != nil {
		st.history = trimHistory(append(st.history, oaMessage{Role: "assistant", Content: text}))
	}
	e.mu.Unlock()
}

// turnOutcome agrupa lo que produjo un turno: intención leída, variables
// escritas, variables descartadas, tools usadas y la versión de configuración
// con la que se comportó el agente. Nació cuando la lista de parámetros de
// turnCompleted dejó de leerse de un vistazo.
type turnOutcome struct {
	intent        string
	confidence    float64
	newVars       []string
	rejectedVars  []string
	toolCalls     []string
	configVersion int
	err           error
}

func (e *GuardianEngine) turnCompleted(convID string, st *guardianConv, started time.Time, out turnOutcome) {
	payload := map[string]interface{}{
		"conversation_id": convID, "user_id": st.userID, "state": string(st.state),
		"intent": out.intent, "confidence": out.confidence,
		"latency_ms_total": time.Since(started).Milliseconds(),
		"tool_calls":       out.toolCalls, "new_variables": out.newVars,
		"rejected_variables": out.rejectedVars,
		"config_version":     out.configVersion, "error": nil,
	}
	if out.err != nil {
		payload["error"] = out.err.Error()
	}
	e.bus.Publish(convID, TURN_COMPLETED, "guardian_engine", payload)
}

func (e *GuardianEngine) fetchQuestions(ctx context.Context, convID string) ([]ProtegeQuestion, error) {
	res := e.tools.Run(ctx, convID, "get_questions", nil)
	if res.Err != nil {
		return nil, res.Err
	}
	qs, _ := res.Data.([]ProtegeQuestion)
	return qs, nil
}

// catalog returns products and rules, fetching what is still missing. Cachea
// solo lo que llegó: un fallo se reintenta en el turno siguiente.
func (e *GuardianEngine) catalog(ctx context.Context, convID string) ([]ProtegeProduct, []ProtegeRule) {
	e.catMu.Lock()
	defer e.catMu.Unlock()
	if len(e.products) == 0 {
		if res := e.tools.Run(ctx, convID, "get_products", nil); res.Err == nil {
			e.products, _ = res.Data.([]ProtegeProduct)
		}
	}
	if len(e.rules) == 0 {
		if res := e.tools.Run(ctx, convID, "get_rules", map[string]interface{}{}); res.Err == nil {
			e.rules, _ = res.Data.([]ProtegeRule)
		}
	}
	return e.products, e.rules
}

// Sweep releases the conversations whose WhatsApp window expired. Se llama
// periódicamente desde main: sin esto el estado de cada conversación
// abandonada (historial, catálogo, recomendaciones) vivía hasta el reinicio.
func (e *GuardianEngine) Sweep() int {
	if e == nil {
		return 0
	}
	expired := e.sessions.Sweep()
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, convID := range expired {
		if _, ok := e.convs[convID]; ok {
			delete(e.convs, convID)
			n++
		}
	}
	return n
}

// State devuelve la etapa viva de una conversación (solo lectura). La usa el
// Playground del Studio para mostrar en qué punto del embudo va la prueba.
func (e *GuardianEngine) State(convID string) LeadState {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if st := e.convs[convID]; st != nil {
		return st.state
	}
	return ""
}

func (e *GuardianEngine) close(convID string) {
	e.mu.Lock()
	delete(e.convs, convID)
	e.mu.Unlock()
	e.sessions.Close(convID)
}

// keyedMutex is a mutex per key: concurrent work on DIFFERENT keys runs in
// parallel, work on the same key is serialized. Las entradas se liberan cuando
// nadie las usa, así que el mapa no crece con cada teléfono que escribe.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// lock takes the key's lock and returns the function that releases it.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = make(map[string]*keyedLock)
	}
	l, ok := k.m[key]
	if !ok {
		l = &keyedLock{}
		k.m[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

// ---- pure helpers ----

var questionMarkers = []string{"?", "¿", "qué", "que es", "cómo", "como funciona", "cuánto", "cuanto", "cuál", "cual", "dónde", "donde", "por qué", "porque", "beneficio", "cubre", "cobertura", "subsidio"}

// looksLikeQuestion is the cheap pre-LLM heuristic that gates RAG retrieval.
func looksLikeQuestion(text string) bool {
	t := strings.ToLower(text)
	for _, m := range questionMarkers {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

var acceptIntents = map[string]bool{"accept": true, "acceptance": true, "request_advisor": true, "handoff": true}

func isAcceptIntent(intent string) bool { return acceptIntents[strings.ToLower(intent)] }
