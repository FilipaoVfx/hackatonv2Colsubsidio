package main

import (
	"time"

	"github.com/google/uuid"
)

// Orchestrator runs one end-to-end call. In the MVP the external providers
// (Vapi / GPT-4o / ElevenLabs) are mocked behind the same event contract, so
// the full pipeline — state machine, feature store, decision, tools,
// recommendation, voice, summary — executes and streams to Mission Control.
// Phase 2 replaces each mock step with a real adapter (ADR-011) with no change
// to this event sequence.
type Orchestrator struct {
	bus  *EventBus
	step time.Duration // pacing so the dashboard shows real-time progression
}

func NewOrchestrator(bus *EventBus, step time.Duration) *Orchestrator {
	return &Orchestrator{bus: bus, step: step}
}

type turn struct {
	user       string
	feature    [3]string // key, value, source ; empty key = skip
	intent     string
	strategy   string
	useTool    bool
	toolName   string
	recommend  bool
	product    [2]string // id, name
	reasoning  string
	factors    []string
	agent      string
	risk       string
	sentiment  string
	tokensIn   int
	tokensOut  int
	latencyMs  int
	costUsd    float64
}

// script is a Family Protector discovery conversation (ADR-010 strategy).
var script = []turn{
	{
		user:      "Hola, vi que ofrecen seguros. Tengo dos hijos pequeños y quiero proteger a mi familia.",
		feature:   [3]string{"family_status", "married_2_children", "transcript"},
		intent:    "interest", strategy: "family_protector",
		agent:     "Claro que sí. Con dos hijos, un seguro de vida familiar es la base. ¿Trabajas de forma independiente o en empresa?",
		risk:      "medium", sentiment: "positive",
		tokensIn:  180, tokensOut: 64, latencyMs: 720, costUsd: 0.0031,
	},
	{
		user:      "Soy independiente, tengo mi propio negocio de logística.",
		feature:   [3]string{"employment", "self_employed", "transcript"},
		intent:    "question", strategy: "family_protector",
		useTool:   true, toolName: "product_search",
		agent:     "Perfecto. Para independientes con familia recomiendo cobertura de vida más un componente de invalidez. Déjame buscar el plan que mejor encaja.",
		risk:      "medium", sentiment: "neutral",
		tokensIn:  240, tokensOut: 78, latencyMs: 910, costUsd: 0.0042,
	},
	{
		user:      "¿Y cuánto costaría más o menos? No quiero algo muy caro.",
		feature:   [3]string{"budget", "cost_sensitive", "inference"},
		intent:    "price_objection", strategy: "family_protector",
		recommend: true, product: [2]string{"COL-VIDA-PLUS-2", "Vida Protección Familiar Plus"},
		reasoning: "Cliente independiente, casado con 2 hijos, sensible al costo: plan con cobertura de vida + invalidez a prima escalonada, sin costo inicial alto.",
		factors:   []string{"family_status", "employment", "budget", "risk_level"},
		agent:     "Entiendo. El Plan Vida Protección Familiar Plus cubre a tus hijos y tiene prima escalonada: empieza bajo y crece contigo. Cuesta menos de lo que imaginas.",
		risk:      "low", sentiment: "positive",
		tokensIn:  310, tokensOut: 96, latencyMs: 1040, costUsd: 0.0058,
	},
	{
		user:      "Suena bien. Me interesa, ¿cómo sigo?",
		intent:    "acceptance", strategy: "family_protector",
		agent:     "Excelente decisión. Te enviaré el resumen con los detalles y un asesor te contactará para formalizar. Gracias por tu tiempo.",
		risk:      "low", sentiment: "positive",
		tokensIn:  260, tokensOut: 70, latencyMs: 680, costUsd: 0.0036,
	},
}

func (o *Orchestrator) Run(from string) string {
	callID := uuid.NewString()
	go o.run(callID, from)
	return callID
}

func (o *Orchestrator) run(callID, from string) {
	b := o.bus
	pause := func() { time.Sleep(o.step) }

	b.Publish(callID, CALL_STARTED, "telephony_adapter", map[string]interface{}{
		"from": from, "vapi_call_id": "mock-" + callID[:8], "channel": "voice",
	})
	o.state(callID, "CREATED", "CONNECTED")
	pause()
	o.state(callID, "CONNECTED", "DISCOVERY")
	pause()

	start := time.Now()
	for _, t := range script {
		b.Publish(callID, USER_SPOKE, "telephony_adapter", map[string]interface{}{
			"audio_ref": "mock://audio/" + uuid.NewString()[:8], "is_final": true,
		})
		o.state(callID, "DISCOVERY", "LISTENING")
		b.Publish(callID, TRANSCRIPT_UPDATED, "telephony_adapter", map[string]interface{}{
			"role": "user", "text": t.user, "is_final": true,
		})
		pause()

		if t.feature[0] != "" {
			b.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
				"key": t.feature[0], "value": t.feature[1], "previous": nil, "source": t.feature[2],
			})
		}
		if t.intent != "" {
			b.Publish(callID, INTENT_DETECTED, "decision_engine", map[string]interface{}{
				"intent": t.intent, "confidence": 0.88,
			})
		}
		if t.risk != "" {
			b.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
				"key": "risk_level", "value": t.risk, "previous": nil, "source": "inference",
			})
		}
		pause()

		// LLM turn (THINKING)
		b.Publish(callID, LLM_REQUESTED, "decision_engine", map[string]interface{}{
			"prompt_ref": "mock://prompt/" + uuid.NewString()[:8], "strategy": t.strategy,
		})
		o.state(callID, "LISTENING", "THINKING")
		pause()

		if t.useTool {
			b.Publish(callID, TOOL_CALLED, "decision_engine", map[string]interface{}{
				"tool": t.toolName, "args": map[string]interface{}{"segment": "family", "type": t.strategy},
			})
			o.state(callID, "THINKING", "TOOL_EXECUTION")
			pause()
			b.Publish(callID, TOOL_EXECUTED, "tool_engine", map[string]interface{}{
				"tool": t.toolName, "latency_ms": 320, "error": nil,
				"result": map[string]interface{}{"matches": 3, "top": "COL-VIDA-PLUS-2"},
			})
			b.Publish(callID, LLM_REQUESTED, "decision_engine", map[string]interface{}{
				"prompt_ref": "mock://prompt/" + uuid.NewString()[:8], "strategy": t.strategy,
			})
			o.state(callID, "TOOL_EXECUTION", "THINKING")
			pause()
		}

		b.Publish(callID, LLM_RESPONSE, "llm_gateway", map[string]interface{}{
			"text": t.agent, "tool_calls": []interface{}{},
			"tokens_in": t.tokensIn, "tokens_out": t.tokensOut,
			"cost_usd": t.costUsd, "latency_ms": t.latencyMs, "model": "gpt-4o",
		})
		pause()

		if t.recommend {
			b.Publish(callID, RECOMMENDATION_GENERATED, "decision_engine", map[string]interface{}{
				"product_id": t.product[0], "product_name": t.product[1],
				"reasoning": t.reasoning, "profile_factors": t.factors, "confidence": 0.91,
			})
			pause()
		}

		b.Publish(callID, VOICE_SENT, "voice_adapter", map[string]interface{}{
			"text": t.agent, "audio_ref": "mock://tts/" + uuid.NewString()[:8],
			"latency_ms": 210, "voice_id": "elevenlabs-es-female-1",
		})
		o.state(callID, "RESPONDING", "LISTENING")
		b.Publish(callID, TRANSCRIPT_UPDATED, "voice_adapter", map[string]interface{}{
			"role": "agent", "text": t.agent, "is_final": true,
		})
		if t.sentiment != "" {
			b.Publish(callID, FEATURE_UPDATED, "feature_store", map[string]interface{}{
				"key": "sentiment", "value": t.sentiment, "previous": nil, "source": "inference",
			})
		}
		pause()
	}

	dur := time.Since(start).Milliseconds()
	b.Publish(callID, SUMMARY_GENERATED, "decision_engine", map[string]interface{}{
		"summary": "Cliente independiente, casado, 2 hijos, sensible al costo. Aceptó Plan Vida Protección Familiar Plus. Riesgo final: bajo. Sentimiento: positivo.",
		"profile_snapshot": map[string]interface{}{
			"family_status": "married_2_children", "employment": "self_employed",
			"budget": "cost_sensitive", "risk_level": "low", "sentiment": "positive",
		},
		"recommendations": []interface{}{"COL-VIDA-PLUS-2"},
		"total_cost_usd":  0.0167, "total_tokens": 1596, "duration_ms": dur,
	})
	b.Publish(callID, CALL_ENDED, "conversation_engine", map[string]interface{}{
		"reason": "system", "duration_ms": dur,
	})
	o.state(callID, "RESPONDING", "ENDED")
}

func (o *Orchestrator) state(callID, from, to string) {
	o.bus.Publish(callID, STATE_CHANGED, "conversation_engine", map[string]interface{}{
		"from": from, "to": to,
	})
}
