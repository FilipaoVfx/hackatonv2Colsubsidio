package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-resty/resty/v2"
)

var (
	ErrOffline  = errors.New("api unreachable")
	ErrNotReady = errors.New("not ready")
)

// APIError wraps a non-2xx response. The backend (Fiber) mostly returns
// fiber.NewError(status, message) -> {"error": "message"}, but some routes
// (analytics) return {"error": "..."} with 503/502 for "supabase not configured".
type APIError struct {
	Status  int
	Message string
	Raw     []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error %d: %s", e.Status, e.Message)
}

func (e *APIError) IsServiceUnavailable() bool { return e.Status == 503 }
func (e *APIError) IsBadGateway() bool         { return e.Status == 502 }
func (e *APIError) IsConflict() bool           { return e.Status == 409 }
func (e *APIError) IsUnprocessable() bool      { return e.Status == 422 }
func (e *APIError) IsNotFound() bool           { return e.Status == 404 }

func parseError(resp *resty.Response) error {
	var env struct {
		Error string `json:"error"`
	}
	body := resp.Body()
	_ = json.Unmarshal(body, &env)
	msg := env.Error
	if msg == "" {
		msg = string(body)
	}
	return &APIError{Status: resp.StatusCode(), Message: msg, Raw: body}
}
