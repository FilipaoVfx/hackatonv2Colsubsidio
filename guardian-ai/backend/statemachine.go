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
	StateReady       LeadState = "READY_FOR_ADVISOR"
	StateNurturing   LeadState = "NURTURING"
	StateCompleted   LeadState = "COMPLETED"
)

// leadTransitions holds the ONLY legal arrows (spec flow + the NURTURING branch
// out of matching when the customer objects).
var leadTransitions = map[LeadState][]LeadState{
	StateNew:         {StateAffiliation},
	StateAffiliation: {StateProfile},
	StateProfile:     {StateFinancial},
	StateFinancial:   {StateMatching},
	StateMatching:    {StateReady, StateNurturing},
	StateReady:       {StateNurturing, StateCompleted},
	StateNurturing:   {StateCompleted},
	StateCompleted:   {},
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
	StateMatching:    {ActionAsk, ActionAnswer, ActionHandoff, ActionClose},
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
		return "Explicar las recomendaciones que entregó el sistema usando EXACTAMENTE sus razones. No inventar productos ni beneficios. Preguntar si desea que un asesor formalice."
	case StateNurturing:
		return "Cerrar con calidez dejando la puerta abierta: se le compartirá información útil y un asesor quedará atento."
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
func MissingQuestions(state LeadState, questions []ProtegeQuestion, known map[string]interface{}) []ProtegeQuestion {
	var out []ProtegeQuestion
	for _, q := range questions {
		if !q.Required {
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
	return len(MissingQuestions(state, questions, known)) == 0
}
