package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// call ejecuta una petición contra el router real y devuelve status + cuerpo.
func call(t *testing.T, mux *http.ServeMux, method, path string, body interface{}) (int, []byte) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// profileUser crea un usuario y le carga un perfil completo de variables.
func profileUser(t *testing.T, mux *http.ServeMux, phone string, vars map[string]interface{}) string {
	t.Helper()
	code, body := call(t, mux, "POST", "/api/v1/users", map[string]string{"phone": phone})
	if code != http.StatusCreated {
		t.Fatalf("crear usuario: %d %s", code, body)
	}
	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		t.Fatalf("decodificar usuario: %v", err)
	}
	var in []UserVariable
	for k, v := range vars {
		in = append(in, UserVariable{Key: k, Value: v, Source: "conversation"})
	}
	if code, body := call(t, mux, "PUT", "/api/v1/users/"+u.ID+"/variables", in); code != http.StatusOK {
		t.Fatalf("cargar variables: %d %s", code, body)
	}
	return u.ID
}

func recommendFor(t *testing.T, mux *http.ServeMux, uid string) []Recommendation {
	t.Helper()
	code, body := call(t, mux, "POST", "/api/v1/recommendations/users/"+uid+"?limit=5", nil)
	if code != http.StatusOK {
		t.Fatalf("recomendar: %d %s", code, body)
	}
	var out struct {
		Recommendations []Recommendation `json:"recommendations"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar recomendaciones: %v", err)
	}
	return out.Recommendations
}

func names(recs []Recommendation) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Name)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

var (
	solteroSinHijos = map[string]interface{}{
		"age": 29.0, "marital_status": "soltero", "has_dependents": false,
		"has_pet": false, "housing_type": "arrendada", "has_vehicle": false,
		"rides_bike": true, "travels_often": true, "travels_abroad": true,
		"has_credit": false, "monthly_income": 3500000.0, "saving_capacity": 90000.0,
	}
	casadoTresHijos = map[string]interface{}{
		"age": 44.0, "marital_status": "casado", "has_dependents": true, "num_dependents": 3.0,
		"has_pet": false, "housing_type": "propia", "has_vehicle": true, "vehicle_type": "carro",
		"rides_bike": false, "travels_often": false, "has_credit": true,
		"monthly_income": 6000000.0, "saving_capacity": 250000.0,
	}
)

// El criterio del reto: la oferta debe ser DISTINTA para un soltero sin hijos y
// para un casado con tres hijos. Aquí se comprueba sobre el motor de reglas
// real, no sobre el texto del LLM.
func TestOfferDiffersByProfile(t *testing.T) {
	mux := routes(newStore())
	solo := names(recommendFor(t, mux, profileUser(t, mux, "+57300", solteroSinHijos)))
	fam := names(recommendFor(t, mux, profileUser(t, mux, "+57301", casadoTresHijos)))

	if len(solo) == 0 || len(fam) == 0 {
		t.Fatalf("algún perfil quedó sin recomendaciones: solo=%v fam=%v", solo, fam)
	}
	if solo[0] == fam[0] {
		t.Errorf("los dos perfiles reciben la misma primera recomendación (%q)", solo[0])
	}
	// Señales propias de cada perfil.
	for _, want := range []string{"Bicicletas", "Asistencia médica en viajes"} {
		if !has(solo, want) {
			t.Errorf("soltero: falta %q en %v", want, solo)
		}
		if has(fam, want) {
			t.Errorf("casado: %q no debería aparecer en %v", want, fam)
		}
	}
	for _, want := range []string{"Seguro de vida"} {
		if !has(fam, want) {
			t.Errorf("casado: falta %q en %v", want, fam)
		}
	}
	// Los productos B2B nunca se ofrecen a una persona natural.
	for _, list := range [][]string{solo, fam} {
		for _, n := range list {
			if strings.Contains(strings.ToLower(n), "empresas") {
				t.Errorf("producto B2B recomendado a persona natural: %q", n)
			}
		}
	}
}

// Cada recomendación viaja con sus coberturas, para que el agente pueda
// explicar y ajustar SIN inventar (RN-005).
func TestRecommendationCarriesCoverages(t *testing.T) {
	mux := routes(newStore())
	recs := recommendFor(t, mux, profileUser(t, mux, "+57302", casadoTresHijos))
	for _, r := range recs {
		if len(r.Coverages) == 0 {
			t.Fatalf("%s llegó sin coberturas", r.Name)
		}
		var optional int
		for _, c := range r.Coverages {
			if !c.Included {
				optional++
			}
			if c.Source == "" {
				t.Errorf("%s: cobertura %q sin origen declarado", r.Name, c.Key)
			}
		}
		if optional == 0 {
			t.Errorf("%s: no hay ninguna cobertura ajustable", r.Name)
		}
	}
}

// Ajustar coberturas cambia el precio de verdad, y el cierre emite una
// solicitud con radicado, resumen y enlace de adquisición.
func TestQuoteAndEnrollmentCloseTheSale(t *testing.T) {
	mux := routes(newStore())
	uid := profileUser(t, mux, "+57303", casadoTresHijos)
	recs := recommendFor(t, mux, uid)
	top := recs[0]

	var optKey string
	var optDelta float64
	for _, c := range top.Coverages {
		if !c.Included {
			optKey, optDelta = c.Key, c.PriceDelta
			break
		}
	}

	code, body := call(t, mux, "POST", "/api/v1/quotes", map[string]interface{}{
		"user_id": uid, "product_id": top.ProductID, "coverages": []string{optKey},
	})
	if code != http.StatusCreated {
		t.Fatalf("cotizar: %d %s", code, body)
	}
	var q Quote
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatalf("decodificar cotización: %v", err)
	}
	if want := q.BasePrice + optDelta; q.MonthlyPrice != want {
		t.Errorf("prima con cobertura añadida = %.0f, esperada %.0f", q.MonthlyPrice, want)
	}

	code, body = call(t, mux, "POST", "/api/v1/enrollments", map[string]interface{}{
		"user_id": uid, "quote_id": q.ID, "accepted": true,
	})
	if code != http.StatusCreated {
		t.Fatalf("vincular: %d %s", code, body)
	}
	var e Enrollment
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decodificar vinculación: %v", err)
	}
	if e.Status != "CONFIRMED" {
		t.Errorf("estado = %q, esperado CONFIRMED", e.Status)
	}
	if !strings.HasPrefix(e.ApplicationNumber, "COL-2026-") {
		t.Errorf("radicado inesperado: %q", e.ApplicationNumber)
	}
	if e.MonthlyPrice != q.MonthlyPrice {
		t.Errorf("la vinculación cobra %.0f y la cotización mostraba %.0f", e.MonthlyPrice, q.MonthlyPrice)
	}
	if !strings.HasPrefix(e.NextStepURL, "https://") {
		t.Errorf("enlace de adquisición inválido: %q", e.NextStepURL)
	}
	if !strings.Contains(e.Summary, e.ApplicationNumber) {
		t.Errorf("el resumen no cita el radicado: %q", e.Summary)
	}

	// Queda consultable por la persona.
	if code, body := call(t, mux, "GET", "/api/v1/users/"+uid+"/enrollments", nil); code != http.StatusOK ||
		!strings.Contains(string(body), e.ApplicationNumber) {
		t.Errorf("las solicitudes del usuario no incluyen la recién creada: %d %s", code, body)
	}
}

// Sin aceptación explícita no hay vinculación, y una cobertura inexistente no
// se cobra en silencio.
func TestEnrollmentGuards(t *testing.T) {
	mux := routes(newStore())
	uid := profileUser(t, mux, "+57304", casadoTresHijos)
	pid := recommendFor(t, mux, uid)[0].ProductID

	if code, _ := call(t, mux, "POST", "/api/v1/enrollments", map[string]interface{}{
		"user_id": uid, "product_id": pid, "accepted": false,
	}); code != http.StatusUnprocessableEntity {
		t.Errorf("vinculación sin aceptación devolvió %d, esperado 422", code)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/quotes", map[string]interface{}{
		"user_id": uid, "product_id": pid, "coverages": []string{"cobertura_inventada"},
	}); code != http.StatusUnprocessableEntity {
		t.Errorf("cobertura inexistente devolvió %d, esperado 422", code)
	}
}

// El catálogo generado tiene que estar completo y coherente.
func TestCatalogIntegrity(t *testing.T) {
	if len(products) != 21 {
		t.Fatalf("productos = %d, esperados 21", len(products))
	}
	seen := map[string]bool{}
	for _, p := range products {
		id, _ := p["id"].(string)
		if id == "" || seen[id] {
			t.Errorf("id vacío o duplicado: %q", id)
		}
		seen[id] = true
		if price, _ := p["base_price"].(float64); price <= 0 {
			t.Errorf("%v sin precio", p["name"])
		}
		meta, _ := p["metadata_json"].(map[string]interface{})
		if src, _ := meta["price_source"].(string); !strings.Contains(src, "estimación de demo") {
			t.Errorf("%v no declara que su prima es estimada", p["name"])
		}
	}
	for key, id := range productIDs {
		if productByID(id) == nil {
			t.Errorf("productIDs[%q] apunta a un producto inexistente", key)
		}
	}
	for _, r := range rulesCatalog {
		if productByID(r.ProductID) == nil {
			t.Errorf("regla %s apunta a un producto inexistente", r.ID)
		}
		if r.Reason == "" {
			t.Errorf("regla %s sin razón: el agente no podría explicar la recomendación", r.ID)
		}
	}
}
