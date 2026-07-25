package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	// per-process catalog cache (products/rules/questions change rarely)
	catOnce  sync.Once
	products []ProtegeProduct
	rules    []ProtegeRule
}

type guardianConv struct {
	userID    string
	phone     string
	state     LeadState
	history   []oaMessage
	questions []ProtegeQuestion
	recs      []string // rendered recommendations shown in MATCHING
	recTries  int      // intentos de get_recommendations en MATCHING
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

const guardianFallbackMsg = "Estoy validando tu información, dame un momento por favor. Si prefieres, un asesor puede continuar contigo."

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
			e.preload(ctx, callID, user.ID, af, "estimación demo (hash de teléfono)")
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
		retrieved = e.rag.Retrieve(ctx, text, 2)
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
		State: st.state, Memory: memory, Products: products, Rules: rules,
		MissingVars: MissingQuestions(st.state, st.questions, known),
		Retrieved:   retrieved, Recs: st.recs,
	})
	st.history = trimHistory(append(st.history, oaMessage{Role: "user", Content: text}))

	e.bus.Publish(convID, LLM_REQUESTED, "guardian_engine", map[string]interface{}{"strategy": string(st.state)})
	d, err := e.llm.DecideGuardian(ctx, prompt, st.history)
	if err != nil {
		e.bus.Publish(convID, ERROR_OCCURRED, "llm_gateway", map[string]interface{}{
			"source": "llm_gateway", "code": "llm_error", "message": err.Error(), "recoverable": true,
		})
		e.sendAgent(convID, phone, guardianFallbackMsg)
		e.turnCompleted(convID, st, started, "", 0, nil, toolCalls, err)
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
	newVars := e.persistEntities(ctx, convID, st, d.Entities, known, runTool)
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
	e.sendAgent(convID, phone, reply)

	// El intent es un enum cerrado del esquema; solo escala si la etapa admite
	// handoff (hoy todas menos las terminales).
	wantsAdvisor := action == ActionHandoff ||
		(isAcceptIntent(d.Intent) && ActionAllowed(st.state, ActionHandoff))
	switch {
	case action == ActionClose:
		e.finishNurturing(ctx, convID, phone, "el cliente cerró la conversación")
	case wantsAdvisor:
		// Pidió un humano: se camina hasta READY por flechas LEGALES, sin
		// inventar una recomendación con un perfil a medias.
		e.escalate(ctx, convID, phone, st, known, runTool)
	case action == ActionRecommend:
		e.fastForward(ctx, convID, phone, st, known, runTool)
	case st.state == StateMatching && len(st.recs) == 0:
		// El intento anterior de recomendar falló: se reintenta en vez de
		// quedarse mudo en MATCHING para siempre.
		e.enterMatching(ctx, convID, phone, st, runTool)
	default:
		e.maybeAdvance(ctx, convID, phone, st, known, runTool)
	}

	e.turnCompleted(convID, st, started, d.Intent, d.Confidence, newVars, toolCalls, nil)
	return nil
}

// preload saves an affiliate profile into the API and emits the features.
// fuente declara el origen en la UI: "estimación demo (hash)" al abrir, o
// "maestro de afiliados (serie confirmada)" cuando el cliente da su número.
func (e *GuardianEngine) preload(ctx context.Context, callID, userID string, af Affiliate, fuente string) {
	conf := 1.0
	vars := append(af.Variables(),
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
		if !serieKeys[strings.ToLower(ent.Key)] || ent.Confidence < 0.6 {
			continue
		}
		if af, ok := e.affiliates.BySerie(fmt.Sprint(ent.Value)); ok {
			e.preload(ctx, callID, st.userID, af, "maestro de afiliados (serie confirmada)")
			return
		}
	}
}

// persistEntities saves confident extracted facts IMMEDIATELY and returns keys.
func (e *GuardianEngine) persistEntities(ctx context.Context, convID string, st *guardianConv,
	entities []GuardianEntity, known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) []string {

	var batch []VariableValue
	var keys []string
	for _, ent := range entities {
		if ent.Key == "" || ent.Confidence < 0.6 {
			continue
		}
		conf := ent.Confidence
		batch = append(batch, VariableValue{Key: ent.Key, Value: ent.Value, Source: "whatsapp", Confidence: &conf})
		keys = append(keys, ent.Key)
	}
	if len(batch) == 0 {
		return nil
	}
	res := runTool("save_variable", map[string]interface{}{"user_id": st.userID, "variables": batch})
	if res.Err != nil {
		return nil // ERROR_OCCURRED ya emitido por el tool engine si aplica
	}
	for _, v := range batch {
		prev := known[v.Key]
		known[v.Key] = v.Value
		e.bus.Publish(convID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
			"key": v.Key, "value": v.Value, "previous": prev, "source": "whatsapp",
		})
	}
	return keys
}

// maybeAdvance walks ONE legal arrow when the current stage is complete.
func (e *GuardianEngine) maybeAdvance(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if !StageComplete(st.state, st.questions, known) {
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
// walking the LEGAL arrows to matching (no skipped states, spec §3.3). Exige
// el perfil descubierto: recomendar sobre un perfil vacío no es acelerar el
// recorrido, es inventarse el match. Si aún falta, se sigue descubriendo.
func (e *GuardianEngine) fastForward(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if st.state == StateProfile && !StageComplete(StateProfile, st.questions, known) {
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
	if st.state != StateMatching {
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
				"Un asesor de Colsubsidio revisará tu caso y te contactará 🙏")
			e.finishNurturing(ctx, convID, phone, "motor de recomendaciones no disponible")
			return
		}
		e.sendAgent(convID, phone, "Estoy generando tu recomendación, dame un momento y seguimos.")
		return
	}
	recs, _ := res.Data.([]interface{})
	st.recs = nil
	lines := []string{"Con base en lo que me contaste, el sistema me sugiere para ti:"}
	for _, r := range recs {
		name, reason := recFields(r)
		if name == "" {
			continue
		}
		e.bus.Publish(convID, RECOMMENDATION_GENERATED, "colsubsidio_api", map[string]interface{}{
			"product_name": name, "reasoning": reason, "product_id": "", "confidence": 0,
		})
		entry := name
		if reason != "" {
			entry += " — " + reason
		}
		st.recs = append(st.recs, entry)
		lines = append(lines, "• "+entry)
	}
	if len(st.recs) == 0 {
		e.sendAgent(convID, phone, "Con tu perfil aún no tengo una recomendación clara; un asesor revisará tu caso y te contactará.")
		e.finishNurturing(ctx, convID, phone, "sin recomendaciones para el perfil")
		return
	}
	lines = append(lines, "\n¿Quieres que un asesor te ayude a formalizar alguna? 😊")
	e.sendAgent(convID, phone, strings.Join(lines, "\n"))
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
	if st.state == StateMatching || st.state == StateReady {
		e.transition(convID, st, StateNurturing, reason)
	} else {
		// legal path: current → ... only NURTURING reachable from MATCHING/READY;
		// from earlier states we record the intent as a state note, not a jump.
		e.bus.Publish(convID, STATE_CHANGED, "guardian_engine", map[string]interface{}{
			"from": string(st.state), "to": string(st.state), "reason": "cierre anticipado: " + reason,
		})
	}
	e.sendAgent(convID, phone, "¡Gracias por tu tiempo! Te compartiré información útil de vez en cuando y un asesor quedará atento a lo que necesites 🌟")
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
	e.bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
		"role": "agent", "text": text, "is_final": true,
	})
	e.bus.Publish(convID, MESSAGE_SENT, "whatsapp_adapter", map[string]interface{}{
		"text": text, "channel": "whatsapp", "status": "queued", "to": phone, "wa_message_id": "",
	})
	e.mu.Lock()
	if st := e.convs[convID]; st != nil {
		st.history = trimHistory(append(st.history, oaMessage{Role: "assistant", Content: text}))
	}
	e.mu.Unlock()
}

func (e *GuardianEngine) turnCompleted(convID string, st *guardianConv, started time.Time,
	intent string, confidence float64, newVars, toolCalls []string, err error) {

	payload := map[string]interface{}{
		"conversation_id": convID, "user_id": st.userID, "state": string(st.state),
		"intent": intent, "confidence": confidence,
		"latency_ms_total": time.Since(started).Milliseconds(),
		"tool_calls":       toolCalls, "new_variables": newVars, "error": nil,
	}
	if err != nil {
		payload["error"] = err.Error()
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

func (e *GuardianEngine) catalog(ctx context.Context, convID string) ([]ProtegeProduct, []ProtegeRule) {
	e.catOnce.Do(func() {
		if res := e.tools.Run(ctx, convID, "get_products", nil); res.Err == nil {
			e.products, _ = res.Data.([]ProtegeProduct)
		}
		if res := e.tools.Run(ctx, convID, "get_rules", map[string]interface{}{}); res.Err == nil {
			e.rules, _ = res.Data.([]ProtegeRule)
		}
	})
	return e.products, e.rules
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
