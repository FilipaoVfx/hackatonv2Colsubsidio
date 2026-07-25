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
	// The API already answered the affiliation question (search result).
	e.bus.Publish(callID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
		"key": "afiliacion", "value": map[bool]string{true: "usuario_nuevo", false: "usuario_existente"}[isNew],
		"previous": nil, "source": "colsubsidio_api",
	})
	e.transitionRaw(callID, StateAffiliation, StateProfile, "identidad resuelta por la API")

	questions, _ := e.fetchQuestions(ctx, callID)
	e.mu.Lock()
	e.convs[callID] = &guardianConv{userID: user.ID, phone: phone, state: StateProfile, questions: questions}
	e.mu.Unlock()
	e.sessions.Register(phone, callID)

	// Afiliado 360: precarga del perfil desde la base de afiliados (solo la
	// primera vez que vemos al usuario; en visitas posteriores las variables ya
	// están en la API). El asesor abre sabiendo lo que Colsubsidio ya sabe.
	if isNew && e.affiliates.Enabled() {
		if af, ok := e.affiliates.ForPhone(phone); ok {
			vars := af.Variables()
			if res := e.tools.Run(ctx, callID, "save_variable",
				map[string]interface{}{"user_id": user.ID, "variables": vars}); res.Err == nil {
				for _, v := range vars {
					e.bus.Publish(callID, FEATURE_UPDATED, "guardian_engine", map[string]interface{}{
						"key": v.Key, "value": v.Value, "previous": nil, "source": "colsubsidio_360",
					})
				}
			}
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

	// 5. Deterministic state advancement (the LLM proposes; the engine decides).
	action := d.NextAction
	if !ActionAllowed(st.state, action) {
		action = ActionAsk
	}
	reply := strings.TrimSpace(d.AssistantMessage)
	if reply == "" {
		reply = guardianFallbackMsg
	}
	e.sendAgent(convID, phone, reply)

	wantsAdvisor := d.NextAction == ActionHandoff || isAcceptIntent(d.Intent)
	switch {
	case action == ActionClose:
		e.finishNurturing(ctx, convID, phone, "el cliente cerró la conversación")
	case st.state == StateMatching && wantsAdvisor:
		e.finishReady(ctx, convID, phone, st, known, runTool)
	case action == ActionRecommend || wantsAdvisor:
		// Cliente pide recomendación/asesor ya: avanza por flechas LEGALES hasta
		// matching (la propuesta de handoff fuera de matching NO salta estados,
		// solo acelera el recorrido legal).
		e.fastForward(ctx, convID, phone, st, known, runTool)
	default:
		e.maybeAdvance(ctx, convID, phone, st, known, runTool)
	}

	e.turnCompleted(convID, st, started, d.Intent, d.Confidence, newVars, toolCalls, nil)
	return nil
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
// walking the LEGAL arrows to matching (no skipped states, spec §3.3).
func (e *GuardianEngine) fastForward(ctx context.Context, convID, phone string, st *guardianConv,
	known map[string]interface{}, runTool func(string, map[string]interface{}) ToolResult) {

	if st.state == StateProfile {
		e.transition(convID, st, StateFinancial, "cliente pidió recomendación")
	}
	if st.state == StateFinancial {
		e.transition(convID, st, StateMatching, "cliente pidió recomendación")
		e.enterMatching(ctx, convID, phone, st, runTool)
	}
}

// enterMatching asks the API for recommendations (the API decides), emits them
// and sends a second LLM-free summary if the LLM cannot be consulted again.
func (e *GuardianEngine) enterMatching(ctx context.Context, convID, phone string, st *guardianConv,
	runTool func(string, map[string]interface{}) ToolResult) {

	res := runTool("get_recommendations", map[string]interface{}{"user_id": st.userID, "limit": 3})
	if res.Err != nil {
		e.sendAgent(convID, phone, "Estoy generando tu recomendación, en un momento te comparto opciones.")
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
