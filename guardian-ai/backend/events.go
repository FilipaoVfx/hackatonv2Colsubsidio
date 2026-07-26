package main

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event names — must match 04_EVENT_CATALOG.md
const (
	CALL_STARTED             = "CALL_STARTED"
	CALL_ENDED               = "CALL_ENDED"
	STATE_CHANGED            = "STATE_CHANGED"
	USER_SPOKE               = "USER_SPOKE"
	TRANSCRIPT_UPDATED       = "TRANSCRIPT_UPDATED"
	INTENT_DETECTED          = "INTENT_DETECTED"
	FEATURE_UPDATED          = "FEATURE_UPDATED"
	LLM_REQUESTED            = "LLM_REQUESTED"
	LLM_RESPONSE             = "LLM_RESPONSE"
	TOOL_CALLED              = "TOOL_CALLED"
	TOOL_EXECUTED            = "TOOL_EXECUTED"
	RECOMMENDATION_GENERATED = "RECOMMENDATION_GENERATED"
	VOICE_SENT               = "VOICE_SENT"
	MESSAGE_RECEIVED         = "MESSAGE_RECEIVED" // chat/WhatsApp: análogo de USER_SPOKE
	MESSAGE_SENT             = "MESSAGE_SENT"     // chat/WhatsApp: análogo de VOICE_SENT
	LEAD_READY               = "LEAD_READY"       // Guardian: lead perfilado listo para asesor
	QUOTE_CREATED            = "QUOTE_CREATED"    // Guardian: cotización mostrada al cliente
	ENROLLMENT_CREATED       = "ENROLLMENT_CREATED" // Guardian: vinculación emitida (queda asegurado)
	KNOWLEDGE_RETRIEVED      = "KNOWLEDGE_RETRIEVED" // Guardian: RAG documental consultado
	TURN_COMPLETED           = "TURN_COMPLETED"   // Guardian: observabilidad por mensaje (spec §11)
	SUMMARY_GENERATED        = "SUMMARY_GENERATED"
	ERROR_OCCURRED           = "ERROR_OCCURRED"
)

// Event is the common envelope defined in the Event Catalog §2.2.
type Event struct {
	EventID   string                 `json:"event_id"`
	Type      string                 `json:"type"`
	CallID    string                 `json:"call_id"`
	Sequence  int                    `json:"sequence"`
	Timestamp string                 `json:"timestamp"`
	Producer  string                 `json:"producer"`
	Payload   map[string]interface{} `json:"payload"`
}

// Handler consumes an event. Consumers treat payload as immutable (invariant §7.3).
type Handler func(Event)

// EventBus is the in-memory pub/sub (ADR-006 / ADR-007). All inter-module
// communication flows through it (RA-002).
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler // by event type; "*" = all events
	seq      map[string]int       // monotonic sequence per call_id (invariant §7.2)
}

func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]Handler),
		seq:      make(map[string]int),
	}
}

// Subscribe registers a handler for a type, or "*" for every event.
func (b *EventBus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

// Publish stamps envelope fields and fans out to subscribers.
func (b *EventBus) Publish(callID, eventType, producer string, payload map[string]interface{}) Event {
	b.mu.Lock()
	seq := b.seq[callID]
	b.seq[callID] = seq + 1
	handlers := append([]Handler{}, b.handlers[eventType]...)
	handlers = append(handlers, b.handlers["*"]...)
	b.mu.Unlock()

	ev := Event{
		EventID:   uuid.NewString(),
		Type:      eventType,
		CallID:    callID,
		Sequence:  seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Producer:  producer,
		Payload:   payload,
	}
	for _, h := range handlers {
		h(ev)
	}
	return ev
}
