package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// LLM Gateway (ADR-012) — real GPT-4o adapter behind a single interface.
// Given the running conversation it returns a structured decision: reply,
// extracted features, intent, risk, sentiment, narrative strategy, optional
// tool call and optional recommendation. This is where "análisis realtime"
// and "data accionable" become real: every field is computed from live input
// and drives the emitted events / next action.

const (
	openAIURL      = "https://api.openai.com/v1/chat/completions"
	openRouterURL  = "https://openrouter.ai/api/v1/chat/completions"
	model          = "gpt-4o"
	priceInPerTok  = 2.50 / 1_000_000  // USD per input token
	priceOutPerTok = 10.0 / 1_000_000  // USD per output token

	// fallbackModel por defecto: anthropic/claude-sonnet-4 — clase GPT-4o
	// (razonamiento comparable, español sólido, structured outputs soportados
	// por OpenRouter) y en infraestructura DISTINTA a OpenAI: una caída de
	// OpenAI no la tumban juntas. Overridable con OPENROUTER_MODEL
	// (p.ej. "openai/gpt-4o" para el modelo idéntico, "google/gemini-2.5-pro").
	fallbackModel = "anthropic/claude-sonnet-4"
)

var systemPrompt = `Eres Guardian AI, un asesor comercial de seguros por voz para Colombia (Colsubsidio).
Conduces la conversación: descubres necesidades, construyes el perfil del cliente y recomiendas un producto justificado.
Estrategias narrativas disponibles: family_protector, pet_owner, professional, entrepreneur, traveler.
Productos de ejemplo: COL-VIDA-PLUS-2 (Vida Protección Familiar Plus), COL-PET-1 (Mascota Segura), COL-PRO-3 (Profesional Integral), COL-TRAVEL-2 (Viajero Global), COL-EMP-1 (Emprendedor Protegido).

En CADA turno respondes SOLO un JSON válido con esta forma exacta:
{
  "reply": "lo que el agente dice al cliente, natural y breve, en español",
  "features": [{"key":"family_status|employment|budget|pets|age_band|health|travel_freq","value":"texto corto"}],
  "intent": "interest|question|price_objection|acceptance|end_call",
  "intent_confidence": 0.0,
  "risk": "low|medium|high",
  "sentiment": "positive|neutral|negative",
  "strategy": "family_protector|pet_owner|professional|entrepreneur|traveler",
  "tool": null,
  "recommendation": null
}
Reglas:
- features: SOLO rasgos nuevos o cambiados detectados en el último mensaje del cliente. Si no hay, lista vacía.
- tool: si necesitas buscar producto, {"name":"product_search","args":{"segment":"..."}}; si no, null.
- recommendation: SOLO cuando ya tienes perfil suficiente para recomendar: {"product_id","product_name","reasoning":"variables del perfil que la justifican","factors":["family_status",...],"confidence":0.0}. Si no, null.
- La recomendación debe ser justificable por el perfil (nunca aleatoria).`

type oaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Decision struct {
	Reply       string `json:"reply"`
	Features    []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"features"`
	Intent     string  `json:"intent"`
	IntentConf float64 `json:"intent_confidence"`
	Risk       string  `json:"risk"`
	Sentiment  string  `json:"sentiment"`
	Strategy   string  `json:"strategy"`
	Tool       *struct {
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	} `json:"tool"`
	Recommendation *struct {
		ProductID   string   `json:"product_id"`
		ProductName string   `json:"product_name"`
		Reasoning   string   `json:"reasoning"`
		Factors     []string `json:"factors"`
		Confidence  float64  `json:"confidence"`
	} `json:"recommendation"`

	// telemetry (filled from usage)
	TokensIn  int     `json:"-"`
	TokensOut int     `json:"-"`
	CostUSD   float64 `json:"-"`
	LatencyMS int64   `json:"-"`
}

type LLMClient struct {
	key     string
	http    *http.Client
	oaURL   string // primario (OpenAI); field para inyectar httptest en tests
	orKey   string // OpenRouter (fallback); vacío = sin fallback
	orURL   string
	orModel string
}

func NewLLMClient() *LLMClient {
	orModel := os.Getenv("OPENROUTER_MODEL")
	if orModel == "" {
		orModel = fallbackModel
	}
	orURL := strings.TrimRight(os.Getenv("OPENROUTER_BASE_URL"), "/")
	if orURL == "" {
		orURL = openRouterURL
	} else {
		orURL += "/chat/completions"
	}
	return &LLMClient{
		key:     os.Getenv("OPENAI_API_KEY"),
		http:    &http.Client{Timeout: 40 * time.Second},
		oaURL:   openAIURL,
		orKey:   os.Getenv("OPENROUTER_API_KEY"),
		orURL:   orURL,
		orModel: orModel,
	}
}

// chatResponse es la forma común de chat completions (OpenAI y OpenRouter
// comparten el contrato; por eso el fallback es un swap de endpoint+modelo).
type chatResponse struct {
	Choices []struct {
		Message oaMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// post ejecuta UNA llamada chat/completions contra url con key.
func (c *LLMClient) post(ctx context.Context, url, key string, body map[string]interface{}) (*chatResponse, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return &out, nil
}

// chat intenta OpenAI primero; si falla (red, 5xx, 429, respuesta vacía) y hay
// OPENROUTER_API_KEY, reintenta el MISMO request en OpenRouter con orModel
// (claude-sonnet-4 por defecto). Sin key de fallback conserva el comportamiento
// original: un solo intento y error propagado.
func (c *LLMClient) chat(ctx context.Context, body map[string]interface{}) (*chatResponse, error) {
	out, err := c.post(ctx, c.oaURL, c.key, body)
	if err == nil {
		return out, nil
	}
	primaryErr := err
	if c.orKey == "" {
		return nil, fmt.Errorf("openai: %w", primaryErr)
	}
	fb := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		fb[k] = v
	}
	fb["model"] = c.orModel
	out, err = c.post(ctx, c.orURL, c.orKey, fb)
	if err != nil {
		return nil, fmt.Errorf("openai: %v | openrouter(%s): %v", primaryErr, c.orModel, err)
	}
	log.Printf("llm: OpenAI falló (%v) — turno respondido por OpenRouter %s", primaryErr, c.orModel)
	return out, nil
}

func (c *LLMClient) Decide(ctx context.Context, history []oaMessage) (*Decision, error) {
	body := map[string]interface{}{
		"model":           model,
		"temperature":     0.6,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        append([]oaMessage{{Role: "system", Content: systemPrompt}}, history...),
	}
	start := time.Now()
	out, err := c.chat(ctx, body)
	if err != nil {
		return nil, err
	}

	var d Decision
	if err := json.Unmarshal([]byte(out.Choices[0].Message.Content), &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w (content=%s)", err, out.Choices[0].Message.Content)
	}
	d.TokensIn = out.Usage.PromptTokens
	d.TokensOut = out.Usage.CompletionTokens
	d.CostUSD = float64(d.TokensIn)*priceInPerTok + float64(d.TokensOut)*priceOutPerTok
	d.LatencyMS = time.Since(start).Milliseconds()
	return &d, nil
}

// ---- Guardian Conversation Engine (spec retrieval.md §8: Structured Outputs) ----

// GuardianEntity is one confirmed fact extracted from the customer's message.
// (json_schema strict forbids free-form objects, so entities is an array.)
type GuardianEntity struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Confidence float64     `json:"confidence"`
}

// GuardianDecision is the ONLY shape the Guardian LLM may answer with.
type GuardianDecision struct {
	Intent           string           `json:"intent"`
	Entities         []GuardianEntity `json:"entities"`
	Confidence       float64          `json:"confidence"`
	NextAction       string           `json:"next_action"`
	AssistantMessage string           `json:"assistant_message"`

	// telemetry (filled from usage)
	TokensIn  int     `json:"-"`
	TokensOut int     `json:"-"`
	CostUSD   float64 `json:"-"`
	LatencyMS int64   `json:"-"`
}

// GuardianLLM is the seam the engine depends on — tests inject a scripted LLM.
type GuardianLLM interface {
	DecideGuardian(ctx context.Context, system string, history []oaMessage) (*GuardianDecision, error)
}

// guardianSchema is the strict JSON Schema enforced server-side by OpenAI
// (response_format json_schema). The model CANNOT answer anything else —
// "nunca parsear texto libre".
var guardianSchema = map[string]interface{}{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"intent", "entities", "confidence", "next_action", "assistant_message"},
	"properties": map[string]interface{}{
		"intent":     map[string]interface{}{"type": "string", "enum": guardianIntents},
		"confidence": map[string]interface{}{"type": "number"},
		"next_action": map[string]interface{}{
			"type": "string",
			"enum": []string{ActionAsk, ActionAnswer, ActionRecommend, ActionHandoff, ActionClose},
		},
		"assistant_message": map[string]interface{}{"type": "string"},
		"entities": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"key", "value", "confidence"},
				"properties": map[string]interface{}{
					"key":        map[string]interface{}{"type": "string"},
					"value":      map[string]interface{}{"type": []string{"string", "number", "boolean"}},
					"confidence": map[string]interface{}{"type": "number"},
				},
			},
		},
	},
}

// DecideGuardian runs one Guardian turn with strict structured outputs. The
// system prompt is built per turn by the Prompt Builder (modular, never global).
func (c *LLMClient) DecideGuardian(ctx context.Context, system string, history []oaMessage) (*GuardianDecision, error) {
	body := map[string]interface{}{
		"model":       model,
		"temperature": 0.5,
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "guardian_turn",
				"strict": true,
				"schema": guardianSchema,
			},
		},
		"messages": append([]oaMessage{{Role: "system", Content: system}}, history...),
	}
	start := time.Now()
	out, err := c.chat(ctx, body)
	if err != nil {
		return nil, err
	}

	var d GuardianDecision
	if err := json.Unmarshal([]byte(out.Choices[0].Message.Content), &d); err != nil {
		return nil, fmt.Errorf("parse guardian decision: %w (content=%s)", err, out.Choices[0].Message.Content)
	}
	d.TokensIn = out.Usage.PromptTokens
	d.TokensOut = out.Usage.CompletionTokens
	d.CostUSD = float64(d.TokensIn)*priceInPerTok + float64(d.TokensOut)*priceOutPerTok
	d.LatencyMS = time.Since(start).Milliseconds()
	return &d, nil
}

// ---- embeddings (RAG, spec §9: documentation only) ----

const (
	openAIEmbedURL = "https://api.openai.com/v1/embeddings"
	embedModel     = "text-embedding-3-small"
)

// Embed returns one vector per input text (batch, single request).
func (c *LLMClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c.key == "" {
		return nil, fmt.Errorf("no OPENAI_API_KEY")
	}
	raw, _ := json.Marshal(map[string]interface{}{"model": embedModel, "input": texts})
	req, _ := http.NewRequestWithContext(ctx, "POST", openAIEmbedURL, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("openai embeddings: %s", out.Error.Message)
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
