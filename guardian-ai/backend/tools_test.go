package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolsUnregisteredRejected(t *testing.T) {
	bus := NewEventBus()
	cap := &capture{}
	bus.Subscribe("*", cap.on)
	tl := NewTools(&ColsubsidioClient{base: "http://127.0.0.1:0", http: http.DefaultClient}, bus)

	res := tl.Run(context.Background(), "c1", "drop_database", nil)
	if res.Err == nil {
		t.Fatal("tool no registrada debe fallar")
	}
	if cap.count(ERROR_OCCURRED) != 1 {
		t.Errorf("ERROR_OCCURRED = %d, want 1", cap.count(ERROR_OCCURRED))
	}
	if cap.count(TOOL_CALLED) != 0 {
		t.Error("tool no registrada jamás emite TOOL_CALLED")
	}
}

func TestToolsEventsAndRetry(t *testing.T) {
	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/questions":
			// primer intento falla, segundo responde (retry idempotente)
			if atomic.AddInt32(&gets, 1) == 1 {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode([]ProtegeQuestion{{ID: "q1"}})
		case "/api/v1/users":
			// POST: si fallara no debe reintentarse
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	bus := NewEventBus()
	cap := &capture{}
	bus.Subscribe("*", cap.on)
	tl := NewTools(&ColsubsidioClient{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}, bus)

	res := tl.Run(context.Background(), "c1", "get_questions", nil)
	if res.Err != nil {
		t.Fatalf("get_questions con retry debía funcionar: %v", res.Err)
	}
	if atomic.LoadInt32(&gets) != 2 {
		t.Errorf("gets = %d, want 2 (1 fallo + 1 retry)", gets)
	}
	if cap.count(TOOL_CALLED) != 1 || cap.count(TOOL_EXECUTED) != 1 {
		t.Errorf("eventos tool = %d/%d, want 1/1", cap.count(TOOL_CALLED), cap.count(TOOL_EXECUTED))
	}

	// POST create_user falla y NO reintenta (no idempotente).
	before := time.Now()
	res = tl.Run(context.Background(), "c1", "create_user", map[string]interface{}{"phone": "+57"})
	_ = before
	if res.Err == nil {
		t.Fatal("create_user contra 500 debe fallar")
	}
	if ev, ok := cap.first(TOOL_EXECUTED); ok && ev.Payload["error"] == nil {
		// el primer TOOL_EXECUTED es el de get_questions (error nil); busca el segundo
	}
	execs := 0
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == TOOL_EXECUTED && ev.Payload["tool"] == "create_user" {
			execs++
			if ev.Payload["error"] == nil {
				t.Error("TOOL_EXECUTED de create_user debe llevar error")
			}
		}
	}
	cap.mu.Unlock()
	if execs != 1 {
		t.Errorf("create_user TOOL_EXECUTED = %d, want 1", execs)
	}
}
