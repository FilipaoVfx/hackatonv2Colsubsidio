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

// VoiceAdapter is the real ElevenLabs TTS adapter (ADR-011). Returns MP3 audio
// for a text reply. Free tier works with premade voices; library voices need a
// paid plan. If the key is unset the dashboard falls back to browser Web Speech.
type VoiceAdapter struct {
	key     string
	voiceID string
	http    *http.Client
}

func NewVoiceAdapter() *VoiceAdapter {
	return &VoiceAdapter{
		key:     os.Getenv("ELEVENLABS_API_KEY"),
		voiceID: os.Getenv("ELEVENLABS_VOICE_ID"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (v *VoiceAdapter) Enabled() bool { return v.key != "" && v.voiceID != "" }

func (v *VoiceAdapter) Synthesize(ctx context.Context, text string) ([]byte, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"text":     text,
		"model_id": "eleven_multilingual_v2",
		"voice_settings": map[string]interface{}{
			"stability": 0.5, "similarity_boost": 0.75,
		},
	})
	url := "https://api.elevenlabs.io/v1/text-to-speech/" + v.voiceID
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("xi-api-key", v.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("elevenlabs %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}
	return data, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
