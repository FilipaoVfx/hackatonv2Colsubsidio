package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ProtegeEngine drives a WhatsApp conversation through the Colsubsidio Protege
// API. The API owns the flow (users, dynamic questions, variables, rule-based
// recommendations); this engine is the channel adapter: it turns inbound
// WhatsApp text into API answers and API questions into outbound WhatsApp text,
// while emitting the same internal events as the voice path so the Pipeline
// analytics keep working unchanged.
//
// The internal call_id IS the API conversation id, so events, sessions and the
// API line up on a single identifier.
type ProtegeEngine struct {
	bus      *EventBus
	api      *ColsubsidioClient
	sessions *WhatsAppSessions

	mu    sync.Mutex
	state map[string]*protegeState // call_id (= API conversation id) -> state
}

type protegeState struct {
	userID   string
	phone    string
	question *ProtegeQuestion // the question currently awaiting an answer
}

func NewProtegeEngine(bus *EventBus, api *ColsubsidioClient, sessions *WhatsAppSessions) *ProtegeEngine {
	return &ProtegeEngine{
		bus:      bus,
		api:      api,
		sessions: sessions,
		state:    make(map[string]*protegeState),
	}
}

func (e *ProtegeEngine) Enabled() bool { return e.api != nil && e.api.Enabled() }

const protegeGreeting = "Hola, soy Guardian AI, tu asesor de seguros de Colsubsidio 🛡️. " +
	"Te haré unas preguntas cortas para recomendarte la mejor protección."

// StartContact opens an autonomous outbound WhatsApp conversation: resolves (or
// creates) the user, opens an API conversation and sends the greeting + first
// question. Returns the conversation id (used as call_id everywhere).
func (e *ProtegeEngine) StartContact(ctx context.Context, phone string) (string, error) {
	user, isNew, err := e.api.ResolveUser(ctx, phone)
	if err != nil {
		return "", err
	}
	conv, err := e.api.StartConversation(ctx, user.ID, phone)
	if err != nil {
		return "", err
	}
	callID := conv.ID

	e.bus.Publish(callID, CALL_STARTED, "whatsapp_adapter", map[string]interface{}{
		"from": phone, "channel": "whatsapp", "user_id": user.ID, "is_new_user": isNew,
	})
	e.bus.Publish(callID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "CREATED", "to": "DISCOVERY"})

	// Publish state BEFORE registering the session so a concurrent HandleInbound
	// that resolves the freshly-registered session always finds its state. The
	// reverse order leaves a window where the session exists but st==nil, which
	// HandleInbound reads as "no pending question" and finishes the call early.
	e.mu.Lock()
	e.state[callID] = &protegeState{userID: user.ID, phone: phone, question: conv.NextQuestion}
	e.mu.Unlock()
	e.sessions.Register(phone, callID)

	opener := protegeGreeting
	if conv.NextQuestion != nil {
		opener += "\n\n" + conv.NextQuestion.Text
	}
	e.sendAgent(callID, phone, opener)

	// Degenerate case: nothing to ask, already recommendable.
	if conv.NextQuestion == nil && conv.CanGenerateRecommendation {
		e.finish(ctx, callID, phone)
	}
	return callID, nil
}

// HandleInbound processes one inbound WhatsApp message. If no live conversation
// exists for the phone it opens one (the message just triggers contact);
// otherwise the text answers the current question and the flow advances.
func (e *ProtegeEngine) HandleInbound(ctx context.Context, phone, text string) (string, error) {
	convID, isNew := e.sessions.Resolve(phone)
	if isNew {
		return e.StartContact(ctx, phone)
	}

	e.bus.Publish(convID, MESSAGE_RECEIVED, "whatsapp_adapter", map[string]interface{}{"is_final": true})
	e.bus.Publish(convID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "DISCOVERY", "to": "LISTENING"})
	e.bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
		"role": "user", "text": text, "is_final": true,
	})

	e.mu.Lock()
	st := e.state[convID]
	e.mu.Unlock()
	if st == nil || st.question == nil {
		// No pending question: try to close out.
		return convID, e.finish(ctx, convID, phone)
	}

	q := st.question
	value := coerceAnswer(q.FieldType, text)

	// Feed the internal profile/analytics with the answered variable.
	e.bus.Publish(convID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
		"key": q.VariableKey, "value": value, "previous": nil, "source": "whatsapp",
	})

	conv, err := e.api.Answer(ctx, convID, q.ID, value, "whatsapp")
	if err != nil {
		// Likely a 422 validation error: re-ask the same question.
		e.bus.Publish(convID, ERROR_OCCURRED, "colsubsidio_api", map[string]interface{}{
			"source": "colsubsidio_api", "code": "answer_rejected", "message": err.Error(), "recoverable": true,
		})
		e.sendAgent(convID, phone, "No logré entender esa respuesta. "+q.Text)
		return convID, nil
	}

	e.mu.Lock()
	if e.state[convID] != nil {
		e.state[convID].question = conv.NextQuestion
	}
	e.mu.Unlock()

	if conv.NextQuestion != nil {
		e.sendAgent(convID, phone, conv.NextQuestion.Text)
		return convID, nil
	}
	if conv.CanGenerateRecommendation || conv.Status == "ready_for_recommendation" || conv.Status == "completed" {
		return convID, e.finish(ctx, convID, phone)
	}
	// No next question yet and not ready: acknowledge.
	e.sendAgent(convID, phone, "Gracias, registré tu respuesta.")
	return convID, nil
}

// finish completes the API conversation, emits the recommendations and closes
// the internal conversation so it projects into the Pipeline.
func (e *ProtegeEngine) finish(ctx context.Context, convID, phone string) error {
	res, err := e.api.Complete(ctx, convID, 3)
	if err != nil {
		e.bus.Publish(convID, ERROR_OCCURRED, "colsubsidio_api", map[string]interface{}{
			"source": "colsubsidio_api", "code": "complete_failed", "message": err.Error(), "recoverable": false,
		})
		e.sendAgent(convID, phone, "Tuvimos un inconveniente generando tu recomendación. Un asesor te contactará.")
		e.close(convID)
		return err
	}

	lines := []string{"Con base en tus respuestas, te recomiendo:"}
	for _, r := range res.Recommendations {
		name, reason := recFields(r)
		if name == "" {
			continue
		}
		e.bus.Publish(convID, RECOMMENDATION_GENERATED, "colsubsidio_api", map[string]interface{}{
			"product_name": name, "reasoning": reason, "product_id": "", "confidence": 0,
		})
		if reason != "" {
			lines = append(lines, "• "+name+" — "+reason)
		} else {
			lines = append(lines, "• "+name)
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "Un asesor revisará tu perfil y te contactará con opciones.")
	}
	lines = append(lines, "\n¿Deseas que un asesor formalice alguna? Responde para continuar.")
	e.sendAgent(convID, phone, strings.Join(lines, "\n"))

	e.bus.Publish(convID, SUMMARY_GENERATED, "colsubsidio_api", map[string]interface{}{
		"summary": fmt.Sprintf("Conversación WhatsApp completada; %d recomendación(es).", len(res.Recommendations)),
	})
	e.bus.Publish(convID, CALL_ENDED, "conversation_engine", map[string]interface{}{"reason": "completed"})
	e.bus.Publish(convID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "RESPONDING", "to": "ENDED"})
	e.close(convID)
	return nil
}

func (e *ProtegeEngine) close(convID string) {
	e.mu.Lock()
	delete(e.state, convID)
	e.mu.Unlock()
	e.sessions.Close(convID)
}

// sendAgent emits the outbound agent message (transcript + MESSAGE_SENT). The
// MESSAGE_SENT consumer in main.go delivers it via Kapso when configured.
func (e *ProtegeEngine) sendAgent(convID, phone, text string) {
	e.bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
		"role": "agent", "text": text, "is_final": true,
	})
	e.bus.Publish(convID, MESSAGE_SENT, "whatsapp_adapter", map[string]interface{}{
		"text": text, "channel": "whatsapp", "status": "queued", "to": phone, "wa_message_id": "",
	})
	e.bus.Publish(convID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "RESPONDING", "to": "LISTENING"})
}

// ---- pure helpers (unit-testable) ----

// numRe grabs a whole number token including Colombian separators (dot=thousands,
// comma=decimal), e.g. "1.500.000" or "2,5".
var numRe = regexp.MustCompile(`-?\d[\d.,]*`)

// firstWord returns the first whitespace/punctuation-delimited token of s,
// lowercased and stripped of surrounding Spanish punctuation.
func firstWord(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == ';' || r == '!' || r == '¡' || r == '?' || r == '¿'
	})
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// coerceAnswer maps free-form WhatsApp text to a typed value matching the
// question's field_type. Best-effort: on a miss it returns the trimmed text and
// lets the API validate (a 422 re-asks the question).
func coerceAnswer(fieldType, text string) interface{} {
	t := strings.TrimSpace(text)
	switch fieldType {
	case "boolean":
		// Heurística de primera palabra: la gente responde "sí, dos hijos" o
		// "no, no tengo", no solo "sí"/"no" a secas. Sin match, pasa el texto
		// crudo y la API valida (422 -> re-pregunta; hook pendiente: GPT NLU).
		switch firstWord(t) {
		case "sí", "si", "s", "yes", "y", "true", "1", "claro", "correcto", "efectivamente", "exacto":
			return true
		case "no", "n", "false", "0", "negativo", "nop", "nunca", "ninguno", "ninguna", "todavia", "todavía":
			return false
		}
		return t
	case "number", "currency":
		if m := numRe.FindString(t); m != "" {
			m = strings.ReplaceAll(m, ".", "") // dots are thousands separators (CO)
			m = strings.ReplaceAll(m, ",", ".") // comma is the decimal mark (CO)
			if f, err := strconv.ParseFloat(m, 64); err == nil {
				return f
			}
		}
		return t
	default: // text, textarea, email, phone, date, radio, select, multi_select
		return t
	}
}

// recFields best-effort extracts a display name and reason from an untyped
// recommendation entry (the API declares recommendations as an untyped array).
func recFields(r interface{}) (name, reason string) {
	m, ok := r.(map[string]interface{})
	if !ok {
		if s, ok := r.(string); ok {
			return s, ""
		}
		return "", ""
	}
	for _, k := range []string{"name", "product_name", "title", "code"} {
		if v, ok := m[k].(string); ok && v != "" {
			name = v
			break
		}
	}
	if p, ok := m["product"].(map[string]interface{}); ok && name == "" {
		if v, ok := p["name"].(string); ok {
			name = v
		}
	}
	for _, k := range []string{"reason", "reasoning", "description"} {
		if v, ok := m[k].(string); ok && v != "" {
			reason = v
			break
		}
	}
	return name, reason
}
