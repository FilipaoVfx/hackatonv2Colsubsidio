package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	model          = "gpt-4o"
	priceInPerTok  = 2.50 / 1_000_000  // USD per input token
	priceOutPerTok = 10.0 / 1_000_000  // USD per output token
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
	key  string
	http *http.Client
}

func NewLLMClient() *LLMClient {
	return &LLMClient{key: os.Getenv("OPENAI_API_KEY"), http: &http.Client{Timeout: 40 * time.Second}}
}

func (c *LLMClient) Decide(ctx context.Context, history []oaMessage) (*Decision, error) {
	body := map[string]interface{}{
		"model":           model,
		"temperature":     0.6,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        append([]oaMessage{{Role: "system", Content: systemPrompt}}, history...),
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", openAIURL, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
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
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("openai: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty response")
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
