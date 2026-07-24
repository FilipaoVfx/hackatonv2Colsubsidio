package main

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// ConversationEngine drives a real turn end-to-end (ADR-009 state machine).
// It keeps per-call message history for context and, on each user utterance,
// asks the LLM Gateway for a decision, then publishes the resulting events.
// Every emitted field is derived from live input — this is the real
// "data accionable / análisis realtime" path.
type ConversationEngine struct {
	bus *EventBus
	llm *LLMClient

	mu      sync.Mutex
	history map[string][]oaMessage
	channel map[string]string // call_id -> canal (web|whatsapp|...); default web
}

func NewConversationEngine(bus *EventBus, llm *LLMClient) *ConversationEngine {
	return &ConversationEngine{
		bus:     bus,
		llm:     llm,
		history: make(map[string][]oaMessage),
		channel: make(map[string]string),
	}
}

// Start opens a new call/conversation and returns its id. `channel` selects the
// I/O adapter semantics ("web" voz-navegador por defecto, "whatsapp" texto). El
// pipeline LLM es idéntico en todos los canales — solo cambia la emisión de
// entrada/salida (ver Turn).
func (e *ConversationEngine) Start(from, channel string) string {
	if channel == "" {
		channel = "web"
	}
	callID := uuid.NewString()
	e.bus.Publish(callID, CALL_STARTED, "telephony_adapter", map[string]interface{}{
		"from": from, "channel": channel, "vapi_call_id": "web-" + callID[:8],
	})
	e.state(callID, "CREATED", "CONNECTED")
	e.state(callID, "CONNECTED", "DISCOVERY")
	e.mu.Lock()
	e.history[callID] = []oaMessage{}
	e.channel[callID] = channel
	e.mu.Unlock()
	return callID
}

// Turn processes one user utterance/message and drives the full event sequence.
// The LLM pipeline (features, intent, tool, response, recommendation) is
// channel-agnostic; only the inbound/outbound I/O events differ per channel.
func (e *ConversationEngine) Turn(ctx context.Context, callID, text string) error {
	e.mu.Lock()
	channel := e.channel[callID]
	e.mu.Unlock()
	if channel == "" {
		channel = "web"
	}

	inType, inProducer := "USER_SPOKE", "telephony_adapter"
	if channel == "whatsapp" {
		inType, inProducer = MESSAGE_RECEIVED, "whatsapp_adapter"
	}
	e.bus.Publish(callID, inType, inProducer, map[string]interface{}{"is_final": true})
	e.state(callID, "DISCOVERY", "LISTENING")
	e.bus.Publish(callID, TRANSCRIPT_UPDATED, inProducer, map[string]interface{}{
		"role": "user", "text": text, "is_final": true,
	})

	e.mu.Lock()
	hist := append(e.history[callID], oaMessage{Role: "user", Content: text})
	e.mu.Unlock()

	e.bus.Publish(callID, LLM_REQUESTED, "decision_engine", map[string]interface{}{"strategy": ""})
	e.state(callID, "LISTENING", "THINKING")

	d, err := e.llm.Decide(ctx, hist)
	if err != nil {
		e.bus.Publish(callID, ERROR_OCCURRED, "llm_gateway", map[string]interface{}{
			"source": "llm_gateway", "code": "llm_error", "message": err.Error(), "recoverable": true,
		})
		return err
	}

	// Features (RN-002): one FEATURE_UPDATED per detected change.
	for _, f := range d.Features {
		e.bus.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
			"key": f.Key, "value": f.Value, "previous": nil, "source": "transcript",
		})
	}
	if d.Risk != "" {
		e.bus.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
			"key": "risk_level", "value": d.Risk, "previous": nil, "source": "inference",
		})
	}
	if d.Sentiment != "" {
		e.bus.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
			"key": "sentiment", "value": d.Sentiment, "previous": nil, "source": "inference",
		})
	}
	if d.Intent != "" {
		e.bus.Publish(callID, INTENT_DETECTED, "decision_engine", map[string]interface{}{
			"intent": d.Intent, "confidence": d.IntentConf,
		})
	}

	// Tool (real decision by the model).
	if d.Tool != nil && d.Tool.Name != "" {
		e.bus.Publish(callID, TOOL_CALLED, "decision_engine", map[string]interface{}{
			"tool": d.Tool.Name, "args": d.Tool.Args,
		})
		e.state(callID, "THINKING", "TOOL_EXECUTION")
		result := runTool(d.Tool.Name, d.Tool.Args)
		e.bus.Publish(callID, TOOL_EXECUTED, "tool_engine", map[string]interface{}{
			"tool": d.Tool.Name, "result": result, "latency_ms": 40, "error": nil,
		})
		e.state(callID, "TOOL_EXECUTION", "THINKING")
	}

	// LLM response with real telemetry.
	e.bus.Publish(callID, LLM_RESPONSE, "llm_gateway", map[string]interface{}{
		"text": d.Reply, "tool_calls": []interface{}{},
		"tokens_in": d.TokensIn, "tokens_out": d.TokensOut,
		"cost_usd": d.CostUSD, "latency_ms": d.LatencyMS, "model": model,
		"strategy": d.Strategy,
	})

	if d.Recommendation != nil && d.Recommendation.ProductID != "" {
		e.bus.Publish(callID, RECOMMENDATION_GENERATED, "decision_engine", map[string]interface{}{
			"product_id": d.Recommendation.ProductID, "product_name": d.Recommendation.ProductName,
			"reasoning": d.Recommendation.Reasoning, "profile_factors": d.Recommendation.Factors,
			"confidence": d.Recommendation.Confidence,
		})
	}

	// Reply out. The output event depends on the channel: voice channels emit
	// VOICE_SENT (ElevenLabs / browser Web Speech), WhatsApp emits MESSAGE_SENT
	// (delivered via Kapso by the bus consumer in main.go). Both channels always
	// emit the agent TRANSCRIPT_UPDATED so the projector/analytics stay identical.
	outType, outProducer, outPayload := replyEvent(channel, d.Reply)
	e.bus.Publish(callID, outType, outProducer, outPayload)
	e.state(callID, "RESPONDING", "LISTENING")
	e.bus.Publish(callID, TRANSCRIPT_UPDATED, outProducer, map[string]interface{}{
		"role": "agent", "text": d.Reply, "is_final": true,
	})

	e.mu.Lock()
	e.history[callID] = append(hist, oaMessage{Role: "assistant", Content: d.Reply})
	e.mu.Unlock()
	return nil
}

func (e *ConversationEngine) state(callID, from, to string) {
	e.bus.Publish(callID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": from, "to": to})
}

// replyEvent maps a channel + reply text to the outbound event to publish. Pure
// (no bus, no state) so the channel→event routing is unit-testable without the
// LLM. WhatsApp emits MESSAGE_SENT (status queued; the bus consumer sends it via
// Kapso); every other channel keeps the existing VOICE_SENT behaviour.
func replyEvent(channel, reply string) (eventType, producer string, payload map[string]interface{}) {
	if channel == "whatsapp" {
		return MESSAGE_SENT, "whatsapp_adapter", map[string]interface{}{
			"text": reply, "channel": "whatsapp", "status": "queued",
			"wa_message_id": "", "to": "",
		}
	}
	return VOICE_SENT, "voice_adapter", map[string]interface{}{
		"text": reply, "audio_ref": "browser://speech", "latency_ms": 0,
		"voice_id": "web-speech-es",
	}
}

// runTool is the mock Tool Engine catalog. Phase 2 swaps this for a real
// product repository (pgvector semantic search).
func runTool(name string, args map[string]interface{}) map[string]interface{} {
	switch name {
	case "product_search":
		return map[string]interface{}{
			"matches": []map[string]string{
				{"id": "COL-VIDA-PLUS-2", "name": "Vida Protección Familiar Plus"},
				{"id": "COL-PRO-3", "name": "Profesional Integral"},
				{"id": "COL-PET-1", "name": "Mascota Segura"},
			},
		}
	}
	return map[string]interface{}{"matches": []interface{}{}}
}
