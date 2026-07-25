package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openAIErrServer simula la caída de OpenAI (500 + cuerpo de error).
func openAIErrServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "The server had an error processing your request"},
		})
	}))
}

// openRouterOKServer responde con un completion válido y captura el request.
func openRouterOKServer(t *testing.T, captured *map[string]interface{}, auth *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(captured)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content":
					`{"intent":"interest","entities":[],"confidence":0.9,"next_action":"ask","assistant_message":"¿Qué edad tienes?"}`}},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
}

func TestFallbackToOpenRouterWhenOpenAIFails(t *testing.T) {
	oa := openAIErrServer(t)
	defer oa.Close()
	var captured map[string]interface{}
	var auth string
	or := openRouterOKServer(t, &captured, &auth)
	defer or.Close()

	c := &LLMClient{key: "sk-openai", http: or.Client(), oaURL: oa.URL, orKey: "sk-or-test", orURL: or.URL, orModel: "anthropic/claude-sonnet-4"}
	d, err := c.DecideGuardian(context.Background(), "system", []oaMessage{{Role: "user", Content: "hola"}})
	if err != nil {
		t.Fatalf("DecideGuardian con fallback no debió fallar: %v", err)
	}
	if d.AssistantMessage != "¿Qué edad tienes?" {
		t.Errorf("reply = %q", d.AssistantMessage)
	}
	if d.TokensIn != 10 {
		t.Errorf("tokens_in = %d, want 10 (uso del proveedor fallback)", d.TokensIn)
	}
	// El request al fallback lleva el modelo de OpenRouter, no gpt-4o
	if captured["model"] != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %v, want anthropic/claude-sonnet-4", captured["model"])
	}
	if auth != "Bearer sk-or-test" {
		t.Errorf("Authorization = %q", auth)
	}
	// Structured outputs se preservan en el fallback (mismo contrato)
	if _, ok := captured["response_format"]; !ok {
		t.Errorf("response_format perdido en el fallback")
	}
}

func TestFallbackErrorMentionsBothProviders(t *testing.T) {
	oa := openAIErrServer(t)
	defer oa.Close()
	or := openAIErrServer(t) // OpenRouter también caído
	defer or.Close()

	c := &LLMClient{key: "k", http: or.Client(), oaURL: oa.URL, orKey: "sk-or-test", orURL: or.URL, orModel: "m"}
	_, err := c.DecideGuardian(context.Background(), "s", nil)
	if err == nil {
		t.Fatal("debió fallar con ambos proveedores caídos")
	}
	if !strings.Contains(err.Error(), "openai:") || !strings.Contains(err.Error(), "openrouter") {
		t.Errorf("error debe mencionar ambos proveedores: %v", err)
	}
}

func TestNoFallbackWithoutOpenRouterKey(t *testing.T) {
	oa := openAIErrServer(t)
	defer oa.Close()

	c := &LLMClient{key: "k", http: oa.Client(), oaURL: oa.URL} // sin orKey
	_, err := c.DecideGuardian(context.Background(), "s", nil)
	if err == nil {
		t.Fatal("debió fallar sin key de fallback")
	}
	if strings.Contains(err.Error(), "openrouter") {
		t.Errorf("sin key no debe intentar fallback: %v", err)
	}
	if !strings.Contains(err.Error(), "openai:") {
		t.Errorf("error conserva prefijo openai: %v", err)
	}
}

func TestDecideFallbackAlsoWorks(t *testing.T) {
	// el motor legacy Decide (json_object) pasa por el mismo helper de fallback
	oa := openAIErrServer(t)
	defer oa.Close()
	or := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content":
					`{"reply":"hola","features":[],"intent":"interest","intent_confidence":0.8,"risk":"low","sentiment":"positive","strategy":"family_protector","tool":null,"recommendation":null}`}},
			},
			"usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2},
		})
	}))
	defer or.Close()

	c := &LLMClient{key: "k", http: or.Client(), oaURL: oa.URL, orKey: "sk-or", orURL: or.URL, orModel: "m"}
	d, err := c.Decide(context.Background(), []oaMessage{{Role: "user", Content: "hola"}})
	if err != nil {
		t.Fatalf("Decide con fallback: %v", err)
	}
	if d.Reply != "hola" || d.Intent != "interest" {
		t.Errorf("decision = %+v", d)
	}
}
