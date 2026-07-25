package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------- buttonsForQuestion (pure helper) ----------

func TestButtonsForQuestionBoolean(t *testing.T) {
	q := &ProtegeQuestion{FieldType: "boolean"}
	got := buttonsForQuestion(q)
	if len(got) != 2 || got[0] != "Sí" || got[1] != "No" {
		t.Errorf("boolean buttons = %v, want [Sí No]", got)
	}
}

func TestButtonsForQuestionSelectStringOptions(t *testing.T) {
	// estilo mock: options como strings planos
	q := &ProtegeQuestion{FieldType: "select", Options: []interface{}{"perro", "gato"}}
	got := buttonsForQuestion(q)
	if len(got) != 2 || got[0] != "perro" || got[1] != "gato" {
		t.Errorf("select buttons = %v, want [perro gato]", got)
	}
}

func TestButtonsForQuestionSelectDictOptions(t *testing.T) {
	// estilo API real: options como {"value":..., "label":...} — el botón usa
	// el VALUE para que la validación estricta de la API lo acepte tal cual.
	q := &ProtegeQuestion{FieldType: "select", Options: []interface{}{
		map[string]interface{}{"value": "propia", "label": "Propia"},
		map[string]interface{}{"value": "arrendada", "label": "Arrendada"},
		map[string]interface{}{"value": "familiar", "label": "Familiar"},
	}}
	got := buttonsForQuestion(q)
	if len(got) != 3 || got[0] != "propia" || got[2] != "familiar" {
		t.Errorf("select dict buttons = %v, want [propia arrendada familiar]", got)
	}
}

func TestButtonsForQuestionTooManyOptions(t *testing.T) {
	// WhatsApp permite máx 3 botones: 4 opciones → texto plano
	q := &ProtegeQuestion{FieldType: "select", Options: []interface{}{"a", "b", "c", "d"}}
	if got := buttonsForQuestion(q); got != nil {
		t.Errorf("4 options buttons = %v, want nil", got)
	}
}

func TestButtonsForQuestionNilAndOthers(t *testing.T) {
	if got := buttonsForQuestion(nil); got != nil {
		t.Errorf("nil question = %v, want nil", got)
	}
	for _, ft := range []string{"text", "number", "currency", "multi_select", "date"} {
		q := &ProtegeQuestion{FieldType: ft}
		if got := buttonsForQuestion(q); got != nil {
			t.Errorf("field_type %s = %v, want nil", ft, got)
		}
	}
}

func TestButtonsForQuestionLongTitleRejected(t *testing.T) {
	// Meta limita títulos a 20 chars
	q := &ProtegeQuestion{FieldType: "select", Options: []interface{}{
		"una opción extremadamente larga", "otra",
	}}
	if got := buttonsForQuestion(q); got != nil {
		t.Errorf("long title = %v, want nil (fallback a texto)", got)
	}
}

// ---------- parseKapsoInbound con interactive (button tap) ----------

func TestParseKapsoInboundButtonReply(t *testing.T) {
	body := `{"message":{"id":"wamid.btn1","type":"interactive","from":"573001234567",
		"interactive":{"type":"button_reply","button_reply":{"id":"btn_0","title":"Sí"}}},
		"conversation":{"id":"c1","phone_number":"573001234567"}}`
	payloads := parseKapsoWebhook([]byte(body))
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(payloads))
	}
	from, text, id := parseKapsoInbound(payloads[0])
	if from != "+573001234567" {
		t.Errorf("from = %q", from)
	}
	if text != "Sí" {
		t.Errorf("text = %q, want %q (título del botón)", text, "Sí")
	}
	if id != "wamid.btn1" {
		t.Errorf("msgID = %q", id)
	}
}

func TestProcessKapsoWebhookButtonTap(t *testing.T) {
	// camino completo: webhook con button tap llega a process() como texto
	body := `{"message":{"id":"wamid.btn2","type":"interactive","from":"57300",
		"interactive":{"type":"button_reply","button_reply":{"id":"btn_1","title":"perro"}}},
		"conversation":{"id":"c1","phone_number":"57300"}}`
	var gotFrom, gotText string
	status, debug := processKapsoWebhook([]byte(body), "whatsapp.message.received", "", "", nil,
		func(from, text string) { gotFrom, gotText = from, text })
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if gotText != "perro" {
		t.Errorf("process text = %q, want perro (%s)", gotText, debug)
	}
	if gotFrom != "+57300" {
		t.Errorf("process from = %q", gotFrom)
	}
}

// ---------- SendButtons (payload Meta interactive contra httptest) ----------

func TestSendButtonsPayload(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			t.Errorf("missing X-API-Key header")
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.x"}]}`))
	}))
	defer srv.Close()

	k := &KapsoAdapter{apiKey: "k", phoneNumberID: "123", base: srv.URL, http: srv.Client()}
	_, err := k.SendButtons(context.Background(), "+57 300 1234567", "¿Tienes mascota?", []string{"Sí", "No"})
	if err != nil {
		t.Fatalf("SendButtons error: %v", err)
	}

	if captured["type"] != "interactive" {
		t.Fatalf("type = %v, want interactive", captured["type"])
	}
	if captured["to"] != "573001234567" {
		t.Errorf("to = %v, want digits-only", captured["to"])
	}
	interactive, _ := captured["interactive"].(map[string]interface{})
	if interactive["type"] != "button" {
		t.Errorf("interactive.type = %v, want button", interactive["type"])
	}
	body, _ := interactive["body"].(map[string]interface{})
	if body["text"] != "¿Tienes mascota?" {
		t.Errorf("body.text = %v", body["text"])
	}
	action, _ := interactive["action"].(map[string]interface{})
	buttons, _ := action["buttons"].([]interface{})
	if len(buttons) != 2 {
		t.Fatalf("buttons = %d, want 2", len(buttons))
	}
	first, _ := buttons[0].(map[string]interface{})
	reply, _ := first["reply"].(map[string]interface{})
	if reply["title"] != "Sí" {
		t.Errorf("first button title = %v, want Sí", reply["title"])
	}
}

func TestSendButtonsOutOfRangeFallsBackToText(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.x"}]}`))
	}))
	defer srv.Close()

	k := &KapsoAdapter{apiKey: "k", phoneNumberID: "123", base: srv.URL, http: srv.Client()}
	// 4 botones: fuera de rango Meta → debe degradar a texto plano
	_, err := k.SendButtons(context.Background(), "+57300", "Elige:", []string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatalf("SendButtons error: %v", err)
	}
	if captured["type"] != "text" {
		t.Errorf("type = %v, want text (fallback)", captured["type"])
	}
}
