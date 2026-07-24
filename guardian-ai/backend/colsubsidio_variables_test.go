package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSaveAndGetVariables verifies the exact VariableValueInput payload of
// PUT /users/{id}/variables and the UserVariableRead round-trip.
func TestSaveAndGetVariables(t *testing.T) {
	stored := map[string]UserVariable{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/api/v1/users/u1/variables":
			var in []UserVariable
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			for _, v := range in {
				if v.Key == "" {
					w.WriteHeader(422)
					return
				}
				stored[v.Key] = v
			}
			out := []UserVariable{}
			for _, v := range stored {
				out = append(out, v)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "GET" && r.URL.Path == "/api/v1/users/u1/variables":
			out := []UserVariable{}
			for _, v := range stored {
				out = append(out, v)
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := &ColsubsidioClient{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	conf := 0.9
	saved, err := c.SaveVariables(context.Background(), "u1", []VariableValue{
		{Key: "has_pet", Value: true, Source: "whatsapp", Confidence: &conf},
		{Key: "monthly_income", Value: 3000000.0, Source: "whatsapp", Confidence: &conf},
	})
	if err != nil {
		t.Fatalf("SaveVariables: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved = %d, want 2", len(saved))
	}

	got, err := c.GetVariables(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetVariables: %v", err)
	}
	byKey := map[string]UserVariable{}
	for _, v := range got {
		byKey[v.Key] = v
	}
	if v, ok := byKey["has_pet"]; !ok || v.Value != true || v.Source != "whatsapp" {
		t.Fatalf("has_pet round-trip = %+v", v)
	}
	if v := byKey["monthly_income"]; v.Value != 3000000.0 {
		t.Fatalf("monthly_income = %v, want 3000000", v.Value)
	}
}

// TestGetQuestionsRulesProducts checks the catalog getters decode the real
// schema shapes (conditions, weights, metadata_json).
func TestGetQuestionsRulesProducts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/questions":
			_, _ = w.Write([]byte(`[{"id":"q1","variable_key":"num_dependents","text":"¿Cuántos?","field_type":"number","required":true,"order_index":4,
				"conditions":[{"depends_on_variable_key":"has_dependents","operator":"equals","expected_value":true}]}]`))
		case "/api/v1/rules":
			if r.URL.Query().Get("product_id") != "p1" {
				t.Errorf("product_id = %q, want p1", r.URL.Query().Get("product_id"))
			}
			_, _ = w.Write([]byte(`[{"id":"r1","product_id":"p1","name":"n","variable_key":"age","operator":"gte","expected_value":30,"weight":0.4,"reason":"por edad","active":true}]`))
		case "/api/v1/products":
			_, _ = w.Write([]byte(`[{"id":"p1","code":"C1","name":"Vida","category":"vida","active":true,"base_price":25000,"metadata_json":{"features":["a"]}}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := &ColsubsidioClient{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	qs, err := c.GetQuestions(context.Background())
	if err != nil || len(qs) != 1 {
		t.Fatalf("GetQuestions: %v %d", err, len(qs))
	}
	if qs[0].OrderIndex != 4 || len(qs[0].Conditions) != 1 || qs[0].Conditions[0].Operator != "equals" {
		t.Fatalf("question conditions not decoded: %+v", qs[0])
	}
	rules, err := c.GetRules(context.Background(), "p1")
	if err != nil || len(rules) != 1 || rules[0].Weight != 0.4 || rules[0].Reason != "por edad" {
		t.Fatalf("GetRules: %v %+v", err, rules)
	}
	prods, err := c.GetProducts(context.Background())
	if err != nil || len(prods) != 1 || prods[0].BasePrice != 25000 {
		t.Fatalf("GetProducts: %v %+v", err, prods)
	}
}

// TestGenerateRecommendations checks the POST /recommendations/users/{id} path
// and the envelope decoding.
func TestGenerateRecommendations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/recommendations/users/u1" || r.URL.Query().Get("limit") != "3" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"user_id":"u1","snapshot_id":"s1","recommendations":[{"name":"Vida","reason":"x","score":0.8}]}`))
	}))
	defer srv.Close()

	c := &ColsubsidioClient{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	recs, err := c.GenerateRecommendations(context.Background(), "u1", 3)
	if err != nil || len(recs) != 1 {
		t.Fatalf("GenerateRecommendations: %v %d", err, len(recs))
	}
	name, reason := recFields(recs[0])
	if name != "Vida" || reason != "x" {
		t.Fatalf("recFields = %q %q", name, reason)
	}
}
