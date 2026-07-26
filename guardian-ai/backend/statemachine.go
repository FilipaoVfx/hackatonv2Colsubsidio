package main

import (
	"fmt"
	"strings"
)

// Lead state machine (spec retrieval.md §3.3): every Guardian conversation
// walks these states in order; no arbitrary transitions. The ENGINE decides
// transitions deterministically (variables completed, API results) — the LLM
// never moves the state. Every transition is emitted as STATE_CHANGED and thus
// persisted by the event store ("cada transición deberá registrarse").
type LeadState string

const (
	StateNew         LeadState = "NEW"
	StateAffiliation LeadState = "AFFILIATION_CHECK"
	StateProfile     LeadState = "PROFILE_DISCOVERY"
	StateFinancial   LeadState = "FINANCIAL_QUALIFICATION"
	StateMatching    LeadState = "PROJECT_MATCHING"
	StateClosing     LeadState = "CLOSING"
	StateReady       LeadState = "READY_FOR_ADVISOR"
	StateNurturing   LeadState = "NURTURING"
	StateCompleted   LeadState = "COMPLETED"
)

// leadTransitions holds the ONLY legal arrows.
//
// El camino feliz termina en CLOSING → COMPLETED: la persona queda ASEGURADA
// dentro del chat. READY_FOR_ADVISOR dejó de ser el final normal y quedó como
// EXCEPCIÓN: solo se alcanza cuando el cliente pide un humano de forma
// explícita o cuando el cierre falla y hay que escalar. NURTURING sigue siendo
// la salida cuando el cliente no quiere avanzar.
var leadTransitions = map[LeadState][]LeadState{
	StateNew:         {StateAffiliation},
	StateAffiliation: {StateProfile},
	StateProfile:     {StateFinancial},
	StateFinancial:   {StateMatching},
	StateMatching:    {StateClosing, StateReady, StateNurturing},
	StateClosing:     {StateCompleted, StateReady, StateNurturing},
	StateReady:       {StateNurturing, StateCompleted},
	StateNurturing:   {StateCompleted},
	StateCompleted:   {},
}

// LeadStateOrder es el recorrido canónico del lead, en el orden en que se
// muestra (consola, troquel del chat). Vive aquí, junto a las flechas legales,
// para que añadir un estado y olvidarlo en la consola sea un test en rojo y no
// un panel "Estado real" que miente (TestLeadStateOrderCoversEveryState).
var LeadStateOrder = []LeadState{
	StateNew, StateAffiliation, StateProfile, StateFinancial,
	StateMatching, StateClosing, StateCompleted, StateReady, StateNurturing,
}

// CanTransition reports whether from→to is a legal arrow.
func CanTransition(from, to LeadState) bool {
	for _, s := range leadTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Guardian next_action enum (structured output). The engine validates the
// LLM's proposal against AllowedActions(state); anything else is ignored and
// treated as "ask" (no arbitrary calls, spec §7).
const (
	ActionAsk       = "ask"                    // seguir perfilando naturalmente
	ActionAnswer    = "answer_question"        // el cliente preguntó algo (RAG)
	ActionRecommend = "request_recommendation" // el cliente pide recomendación ya
	ActionAdjust    = "adjust_coverage"        // quiere cambiar de plan o coberturas
	ActionAccept    = "accept_offer"           // acepta y quiere vincularse
	ActionHandoff   = "handoff"                // el cliente quiere un asesor humano
	ActionClose     = "close"                  // el cliente quiere terminar
)

// allowedActions: pedir un asesor humano es legítimo en CUALQUIER etapa, así
// que handoff es legal en todas (el motor sigue caminando solo flechas legales
// para llegar al handoff; nunca salta estados). Declararlo ilegal y honrarlo
// igual dejaba la whitelist como adorno. Y `ask` es legal en MATCHING: el
// agente puede resolver dudas sobre la recomendación antes de cerrar.
var allowedActions = map[LeadState][]string{
	StateAffiliation: {ActionAsk, ActionAnswer, ActionHandoff, ActionClose},
	StateProfile:     {ActionAsk, ActionAnswer, ActionRecommend, ActionHandoff, ActionClose},
	StateFinancial:   {ActionAsk, ActionAnswer, ActionRecommend, ActionHandoff, ActionClose},
	StateMatching:    {ActionAsk, ActionAnswer, ActionAdjust, ActionAccept, ActionHandoff, ActionClose},
	StateClosing:     {ActionAsk, ActionAnswer, ActionAdjust, ActionAccept, ActionHandoff, ActionClose},
	StateNurturing:   {ActionAnswer, ActionClose},
}

// FallbackAction is the action a state degrades to when the LLM proposes one
// outside its whitelist. Devuelve SIEMPRE una acción legal en ese estado
// (degradar a "ask" a ciegas producía una acción prohibida en MATCHING).
func FallbackAction(s LeadState) string {
	for _, candidate := range []string{ActionAsk, ActionAnswer, ActionClose} {
		if ActionAllowed(s, candidate) {
			return candidate
		}
	}
	return ActionClose
}

// guardianIntents is the CLOSED intent vocabulary of the structured output.
// El motor decide escalamientos leyendo `intent`, así que no puede ser texto
// libre: el esquema JSON lo restringe a esta lista.
var guardianIntents = []string{
	"greeting", "provide_info", "ask_info", "objection",
	"accept", "reject", "request_advisor", "goodbye", "other",
}

// AllowedActions returns the next_action whitelist for a state (empty slice
// for terminal / engine-only states).
func AllowedActions(s LeadState) []string {
	if a, ok := allowedActions[s]; ok {
		return a
	}
	return []string{}
}

// ActionAllowed reports whether the LLM-proposed action is legal in the state.
func ActionAllowed(s LeadState, action string) bool {
	for _, a := range AllowedActions(s) {
		if a == action {
			return true
		}
	}
	return false
}

// StateGoal is the per-stage objective injected into the system prompt.
func StateGoal(s LeadState) string {
	switch s {
	case StateAffiliation:
		return "Confirmar la identidad del cliente y su relación con Colsubsidio de forma cálida. Presentarte y generar confianza."
	case StateProfile:
		return "Descubrir el perfil del cliente conversando con naturalidad (familia, mascotas, vivienda, movilidad). Máximo una pregunta por mensaje, ligada a lo que el cliente acaba de contar. NUNCA interrogar."
	case StateFinancial:
		return "Entender con tacto la capacidad financiera (ingresos aproximados y cuánto podría destinar a protección). Normalizar el tema: es para recomendar algo que sí le sirva."
	case StateMatching:
		return "Explicar las recomendaciones que entregó el sistema usando EXACTAMENTE sus razones. No inventar productos, coberturas ni precios. Ofrecer comparar opciones o ajustar coberturas, y proponer el cierre: TÚ formalizas la vinculación aquí mismo, sin derivar a nadie."
	case StateClosing:
		return "Cerrar la vinculación: confirmar el plan cotizado, resolver la última duda y pedir una aceptación explícita. El resumen, el precio y el radicado los entrega el sistema: NUNCA los inventes ni los adelantes."
	case StateNurturing:
		return "Cerrar con calidez dejando la puerta abierta: se le compartirá información útil y podrá retomar la contratación cuando quiera."
	default:
		return "Atender al cliente con calidez y claridad."
	}
}

// financialVarKeys partitions the API question catalog: these variable_keys
// belong to FINANCIAL_QUALIFICATION; every other question belongs to
// PROFILE_DISCOVERY. Heuristic by key naming, with the two known seeds pinned.
func isFinancialVar(key string) bool {
	if key == "monthly_income" || key == "saving_capacity" {
		return true
	}
	k := strings.ToLower(key)
	return strings.Contains(k, "income") || strings.Contains(k, "ingreso") ||
		strings.Contains(k, "saving") || strings.Contains(k, "ahorro") ||
		strings.Contains(k, "salary") || strings.Contains(k, "salario")
}

// condMet evaluates one question condition against the known variables
// (ConditionOperator enum). Unknown variable ⇒ condition unmet.
func condMet(c ProtegeCondition, known map[string]interface{}) bool {
	v, ok := known[c.DependsOnVariableKey]
	switch c.Operator {
	case "exists":
		return ok
	case "equals":
		return ok && looseEq(v, c.ExpectedValue)
	case "not_equals":
		return ok && !looseEq(v, c.ExpectedValue)
	case "gt", "gte", "lt", "lte":
		a, ok1 := asFloat(v)
		b, ok2 := asFloat(c.ExpectedValue)
		if !ok || !ok1 || !ok2 {
			return false
		}
		switch c.Operator {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		case "lte":
			return a <= b
		}
	case "in":
		arr, isArr := c.ExpectedValue.([]interface{})
		if !ok || !isArr {
			return false
		}
		for _, e := range arr {
			if looseEq(v, e) {
				return true
			}
		}
		return false
	case "contains":
		s, ok1 := v.(string)
		sub, ok2 := c.ExpectedValue.(string)
		return ok && ok1 && ok2 && strings.Contains(strings.ToLower(s), strings.ToLower(sub))
	}
	return false
}

// MissingQuestions returns the questions of `state` whose variable is still
// unknown and whose conditions are met — i.e. what the conversation still needs
// to discover. Guidance for the prompt, never a form.
//
// Incluye las OPCIONALES: son las que enriquecen el perfil (vehículo, bici,
// viajes, crédito) y sin ellas el agente nunca preguntaría por los productos
// que dependen de esas variables. Lo que NO hacen es bloquear la etapa: para
// eso está missingRequired.
func MissingQuestions(state LeadState, questions []ProtegeQuestion, known map[string]interface{}) []ProtegeQuestion {
	req := missingQuestions(state, questions, known, true)
	return append(req, missingQuestions(state, questions, known, false)...)
}

// missingQuestions filtra por etapa y, si requiredOnly, solo las obligatorias;
// si no, solo las opcionales.
func missingQuestions(state LeadState, questions []ProtegeQuestion, known map[string]interface{}, requiredOnly bool) []ProtegeQuestion {
	var out []ProtegeQuestion
	for _, q := range questions {
		if q.Required != requiredOnly {
			continue
		}
		fin := isFinancialVar(q.VariableKey)
		if (state == StateFinancial) != fin {
			continue
		}
		if _, have := known[q.VariableKey]; have {
			continue
		}
		met := true
		for _, c := range q.Conditions {
			if !condMet(c, known) {
				met = false
				break
			}
		}
		if met {
			out = append(out, q)
		}
	}
	return out
}

// asFloat coerces JSON-ish values to float64 for numeric comparisons.
func asFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// looseEq compares tolerantly: numbers by value, booleans, else lowercase string.
func looseEq(a, b interface{}) bool {
	if af, ok := asFloat(a); ok {
		if bf, ok2 := asFloat(b); ok2 {
			return af == bf
		}
	}
	if ab, ok := a.(bool); ok {
		if bb, ok2 := b.(bool); ok2 {
			return ab == bb
		}
	}
	return strings.EqualFold(strings.TrimSpace(toStr(a)), strings.TrimSpace(toStr(b)))
}

func toStr(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// StageComplete: the current stage has nothing left to discover.
func StageComplete(state LeadState, questions []ProtegeQuestion, known map[string]interface{}) bool {
	if state != StateProfile && state != StateFinancial {
		return false
	}
	// Sin catálogo de preguntas no hay EVIDENCIA de etapa completa, solo
	// ausencia de datos: si el GET /questions falló, avanzar aquí declararía
	// "perfil completo" con cero variables descubiertas.
	if len(questions) == 0 {
		return false
	}
	// Solo las OBLIGATORIAS deciden si la etapa terminó: las opcionales guían la
	// conversación pero no pueden dejarla atascada preguntando para siempre.
	return len(missingQuestions(state, questions, known, true)) == 0
}
