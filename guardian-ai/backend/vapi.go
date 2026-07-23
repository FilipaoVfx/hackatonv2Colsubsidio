package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// VapiAdapter places real outbound phone calls (ADR-011). The assistant runs
// inside Vapi: GPT-4o brain + Spanish transcriber + voice. The customer talks to
// Guardian AI on a real phone. (Live sync of the call into our event bus/dashboard
// would use Vapi's serverUrl webhook and requires a public HTTPS endpoint — phase 2.)
type VapiAdapter struct {
	key     string
	phoneID string
	http    *http.Client
}

func NewVapiAdapter() *VapiAdapter {
	return &VapiAdapter{
		key:     os.Getenv("VAPI_API_KEY"),
		phoneID: os.Getenv("VAPI_PHONE_ID"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (v *VapiAdapter) Enabled() bool { return v.key != "" && v.phoneID != "" }

const voiceSystemPrompt = `Eres Guardian AI, asesor comercial de seguros de Colsubsidio (Colombia), hablando por teléfono.
Hablas natural, cálido y breve — frases cortas, una idea por turno. Descubres la necesidad del cliente,
identificas su perfil (familia, mascotas, trabajo, viajes, presupuesto) y recomiendas UN producto justificado.
Productos: Vida Protección Familiar Plus, Mascota Segura, Profesional Integral, Viajero Global, Emprendedor Protegido.
Nunca inventes precios exactos; ofrece que un asesor formalice. Cierra agradeciendo.`

// PlaceCall dials `to` (E.164, e.g. +573001234567) and returns the Vapi call id.
func (v *VapiAdapter) PlaceCall(ctx context.Context, to string) (string, error) {
	payload := map[string]interface{}{
		"phoneNumberId": v.phoneID,
		"customer":      map[string]string{"number": to},
		"assistant": map[string]interface{}{
			"firstMessage": "Hola, soy Guardian AI, tu asesor de seguros de Colsubsidio. ¿Con quién tengo el gusto?",
			"model": map[string]interface{}{
				"provider": "openai", "model": "gpt-4o",
				"messages": []map[string]string{{"role": "system", "content": voiceSystemPrompt}},
			},
			"voice":       map[string]interface{}{"provider": "vapi", "voiceId": "Paige"},
			"transcriber": map[string]interface{}{"provider": "deepgram", "language": "es"},
		},
	}
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.vapi.ai/call", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+v.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("vapi %d: %s", resp.StatusCode, string(data[:min(len(data), 300)]))
	}
	var out struct {
		ID string `json:"id"`
	}
	json.Unmarshal(data, &out)
	return out.ID, nil
}

var _ = time.Second
