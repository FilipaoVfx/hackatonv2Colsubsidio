// mock-protege es un mock local de la Colsubsidio Protege API (OpenAPI 0.1.0).
//
// Para efectos de la demo se usa un mock de la API de integración debido a
// restricciones de acceso de red sobre la API real (147.93.11.136:9000 filtra
// nuestra IP de egress). Responde con el MISMO contrato (rutas, schemas y
// códigos de estado) que la API real, así que el resto del sistema — el
// backend Go (colsubsidio.go / protege.go), el canal WhatsApp y el Pipeline —
// no sabe que es un mock: basta apuntar COLSUBSIDIO_API_URL a este servicio.
//
// Fidelidad al motor real:
//   - Productos: datos REALES capturados de GET /api/v1/products (2026-07-24).
//   - Preguntas: schema fiel (variable_key, field_type de FieldType, order_index,
//     validation, conditions con ConditionOperator) y flujo DINÁMICO por
//     order_index + condiciones (branching), igual que el motor real. Los TEXTOS
//     son una réplica plausible del cuestionario (no se capturó el seed real).
//   - Reglas: schema fiel (RuleCreate/RuleRead: product_id, variable_key,
//     operator, expected_value, weight, reason, active) y SCORING PONDERADO
//     (suma de weights de reglas activas que matchean → ranking), igual que el
//     motor real. Los pesos/valores son réplica plausible (no el seed real).
//
// Estado en memoria (se reinicia con el contenedor), igual que el EventStore
// del backend. Sin dependencias externas: solo la stdlib de Go.
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Schemas (subset fiel del OpenAPI 0.1.0)
// ---------------------------------------------------------------------------

type User struct {
	ID             string `json:"id"`
	DocumentType   string `json:"document_type,omitempty"`
	DocumentNumber string `json:"document_number,omitempty"`
	NIT            string `json:"nit,omitempty"`
	Phone          string `json:"phone,omitempty"`
	Email          string `json:"email,omitempty"`
	FirstName      string `json:"first_name,omitempty"`
	LastName       string `json:"last_name,omitempty"`
	IsActive       bool   `json:"is_active"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// Condition = QuestionConditionRead: la pregunta solo se hace si la variable
// de la que depende cumple el operador contra expected_value.
type Condition struct {
	DependsOnVariableKey string      `json:"depends_on_variable_key"`
	Operator             string      `json:"operator"` // ConditionOperator
	ExpectedValue        interface{} `json:"expected_value"`
}

// Question = QuestionPublic + order_index/active/conditions (schema real).
type Question struct {
	ID          string                 `json:"id"`
	VariableKey string                 `json:"variable_key"`
	Text        string                 `json:"text"`
	FieldType   string                 `json:"field_type"` // FieldType enum
	Required    bool                   `json:"required"`
	OrderIndex  int                    `json:"order_index"`
	Active      bool                   `json:"active"`
	HelpText    string                 `json:"help_text"`
	Placeholder string                 `json:"placeholder"`
	Validation  map[string]interface{} `json:"validation"`
	Options     []interface{}          `json:"options"`
	Conditions  []Condition            `json:"conditions"`
}

// UserVariable = UserVariableRead / VariableValueInput (perfil persistido).
type UserVariable struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Source     string      `json:"source"`
	Confidence *float64    `json:"confidence"`
}

// Rule = RuleRead (motor de recomendación por reglas ponderadas).
type Rule struct {
	ID            string      `json:"id"`
	ProductID     string      `json:"product_id"`
	Name          string      `json:"name"`
	VariableKey   string      `json:"variable_key"`
	Operator      string      `json:"operator"` // ConditionOperator
	ExpectedValue interface{} `json:"expected_value"`
	Weight        float64     `json:"weight"`
	Reason        string      `json:"reason"`
	Active        bool        `json:"active"`
}

type Conversation struct {
	ID                        string    `json:"id"`
	UserID                    string    `json:"user_id"`
	Channel                   string    `json:"channel"`
	Status                    string    `json:"status"` // ConversationStatus
	CurrentQuestionID         string    `json:"current_question_id"`
	NextQuestion              *Question `json:"next_question"`
	CanGenerateRecommendation bool      `json:"can_generate_recommendation"`

	answers map[string]interface{} // variable_key -> valor respondido
}

// Recommendation: entrada del array de ConversationCompleteResponse. El backend
// (recFields) lee name/reason; el resto es contexto útil para la UI/jurado.
type Recommendation struct {
	ProductID string  `json:"product_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	BasePrice float64 `json:"base_price"`
	Score     float64 `json:"score"`
	Reason    string  `json:"reason"`
}

// ---------------------------------------------------------------------------
// Catálogo: datos REALES capturados de GET /api/v1/products (2026-07-24)
// ---------------------------------------------------------------------------

const (
	pidHogar     = "0e92218c-3d75-4024-aed2-7ea1cc5f0950"
	pidMascota   = "a49e8875-f8d4-43f3-a9f0-9a144b41a0ff"
	pidAccidente = "759f18ae-a795-449a-b376-c7a7ba6111d4"
	pidVida      = "71d3c72e-47ab-4941-8223-73e14bfa82b9"
)

var products = []map[string]interface{}{
	{
		"code": "HOGAR_PROTEGIDO_TEST", "name": "Seguro Hogar Protegido",
		"description": "Producto de prueba para personas interesadas en proteger su vivienda y los bienes contenidos en ella.",
		"category": "hogar", "active": true, "base_price": 32000.0,
		"metadata_json": map[string]interface{}{
			"test_product": true, "target_profile": "Propietarios o arrendatarios interesados en protección del hogar",
			"features":   []string{"Protección de vivienda", "Protección de bienes del hogar"},
			"disclaimer": "Producto creado únicamente para pruebas del motor de recomendaciones.",
		},
		"id": pidHogar,
		"created_at": "2026-07-24T02:21:06.185867Z", "updated_at": "2026-07-24T02:21:06.185867Z",
	},
	{
		"code": "MASCOTA_PROTEGIDA_TEST", "name": "Seguro Mascota Protegida",
		"description": "Producto de prueba dirigido a personas que tienen mascota y desean contar con respaldo para su cuidado.",
		"category": "mascotas", "active": true, "base_price": 22000.0,
		"metadata_json": map[string]interface{}{
			"test_product": true, "target_profile": "Personas con perros o gatos",
			"features":   []string{"Respaldo para el cuidado de mascotas", "Orientado a propietarios de mascotas"},
			"disclaimer": "Producto creado únicamente para pruebas del motor de recomendaciones.",
		},
		"id": pidMascota,
		"created_at": "2026-07-24T02:21:19.415377Z", "updated_at": "2026-07-24T02:21:19.415377Z",
	},
	{
		"code": "ACCIDENTES_PERSONALES_TEST", "name": "Seguro de Accidentes Personales",
		"description": "Producto de prueba para personas que se desplazan con frecuencia y desean protección frente a accidentes personales.",
		"category": "accidentes", "active": true, "base_price": 18000.0,
		"metadata_json": map[string]interface{}{
			"test_product": true, "target_profile": "Personas con desplazamientos frecuentes",
			"features":   []string{"Protección frente a accidentes", "Orientado a personas activas"},
			"disclaimer": "Producto creado únicamente para pruebas del motor de recomendaciones.",
		},
		"id": pidAccidente,
		"created_at": "2026-07-24T02:20:52.086996Z", "updated_at": "2026-07-24T02:20:52.086996Z",
	},
	{
		"code": "VIDA_FAMILIAR_TEST", "name": "Seguro de Vida Familiar",
		"description": "Producto de prueba orientado a personas que desean brindar respaldo económico a su familia ante eventos inesperados.",
		"category": "vida", "active": true, "base_price": 25000.0,
		"metadata_json": map[string]interface{}{
			"test_product": true, "target_profile": "Personas con familiares o dependientes económicos",
			"features":   []string{"Protección económica familiar", "Orientado a personas con dependientes"},
			"disclaimer": "Producto creado únicamente para pruebas del motor de recomendaciones.",
		},
		"id": pidVida,
		"created_at": "2026-07-24T02:20:42.767413Z", "updated_at": "2026-07-24T02:20:42.767413Z",
	},
}

func productByID(id string) map[string]interface{} {
	for _, p := range products {
		if p["id"] == id {
			return p
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Preguntas dinámicas (schema fiel; flujo por order_index + conditions)
// ---------------------------------------------------------------------------

var questionsCatalog = []Question{
	{ID: "q_name", VariableKey: "full_name", Text: "¿Cuál es tu nombre completo?",
		FieldType: "text", Required: true, OrderIndex: 1, Active: true,
		Placeholder: "Ej: Ana María Rojas", Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	{ID: "q_age", VariableKey: "age", Text: "¿Qué edad tienes?",
		FieldType: "number", Required: true, OrderIndex: 2, Active: true,
		HelpText: "En años cumplidos", Placeholder: "Ej: 34",
		Validation: map[string]interface{}{"min": 18.0, "max": 100.0}, Options: []interface{}{}, Conditions: []Condition{}},

	{ID: "q_dependents", VariableKey: "has_dependents", Text: "¿Tienes hijos o dependientes económicos?",
		FieldType: "boolean", Required: true, OrderIndex: 3, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si has_dependents == true (branching real).
	{ID: "q_num_dependents", VariableKey: "num_dependents", Text: "¿Cuántas personas dependen económicamente de ti?",
		FieldType: "number", Required: true, OrderIndex: 4, Active: true,
		HelpText: "Número de dependientes", Placeholder: "Ej: 2",
		Validation: map[string]interface{}{"min": 1.0, "max": 15.0}, Options: []interface{}{},
		Conditions: []Condition{{DependsOnVariableKey: "has_dependents", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_pet", VariableKey: "has_pet", Text: "¿Tienes perro o gato en casa?",
		FieldType: "boolean", Required: true, OrderIndex: 5, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si has_pet == true.
	{ID: "q_pet_type", VariableKey: "pet_type", Text: "¿Es perro o gato?",
		FieldType: "select", Required: true, OrderIndex: 6, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{"perro", "gato"},
		Conditions: []Condition{{DependsOnVariableKey: "has_pet", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_housing", VariableKey: "housing_type", Text: "¿Vives en casa propia, arrendada o familiar?",
		FieldType: "select", Required: true, OrderIndex: 7, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{"propia", "arrendada", "familiar"}, Conditions: []Condition{}},

	{ID: "q_travel", VariableKey: "travels_often", Text: "¿Te desplazas con frecuencia por trabajo o estudio?",
		FieldType: "boolean", Required: true, OrderIndex: 8, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Calificación financiera (FINANCIAL_QUALIFICATION del Guardian engine).
	{ID: "q_income", VariableKey: "monthly_income", Text: "¿Cuáles son tus ingresos mensuales aproximados?",
		FieldType: "currency", Required: true, OrderIndex: 9, Active: true,
		HelpText: "En pesos colombianos", Placeholder: "Ej: 2.500.000",
		Validation: map[string]interface{}{"min": 0.0}, Options: []interface{}{}, Conditions: []Condition{}},

	{ID: "q_savings", VariableKey: "saving_capacity", Text: "¿Cuánto podrías destinar al mes a tu protección?",
		FieldType: "currency", Required: true, OrderIndex: 10, Active: true,
		HelpText: "Capacidad de ahorro mensual", Placeholder: "Ej: 100.000",
		Validation: map[string]interface{}{"min": 0.0}, Options: []interface{}{}, Conditions: []Condition{}},
}

// ---------------------------------------------------------------------------
// Reglas de recomendación (schema fiel; scoring ponderado)
// ---------------------------------------------------------------------------

var rulesCatalog = []Rule{
	// Vida Familiar
	{ID: "r_vida_dep", ProductID: pidVida, Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.6, Active: true,
		Reason: "Tienes dependientes económicos: este seguro les da respaldo ante eventos inesperados."},
	{ID: "r_vida_numdep", ProductID: pidVida, Name: "Varios dependientes", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 2.0, Weight: 0.3, Active: true,
		Reason: "Tienes varios dependientes: conviene un respaldo económico familiar más amplio."},
	{ID: "r_vida_edad", ProductID: pidVida, Name: "Edad de responsabilidad", VariableKey: "age",
		Operator: "gte", ExpectedValue: 30.0, Weight: 0.1, Active: true,
		Reason: "Por tu etapa de vida, un seguro de vida familiar protege a los tuyos."},

	// Mascota Protegida
	{ID: "r_masc_has", ProductID: pidMascota, Name: "Tiene mascota", VariableKey: "has_pet",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Tienes mascota en casa: cubre el cuidado de tu perro o gato."},
	{ID: "r_masc_tipo", ProductID: pidMascota, Name: "Perro o gato", VariableKey: "pet_type",
		Operator: "in", ExpectedValue: []interface{}{"perro", "gato"}, Weight: 0.2, Active: true,
		Reason: "Tu mascota entra en la cobertura del plan."},

	// Hogar Protegido
	{ID: "r_hogar_propia", ProductID: pidHogar, Name: "Vivienda propia", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "propia", Weight: 0.6, Active: true,
		Reason: "Vives en casa propia: protege tu vivienda y los bienes contenidos en ella."},
	{ID: "r_hogar_arr", ProductID: pidHogar, Name: "Vivienda arrendada", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "arrendada", Weight: 0.4, Active: true,
		Reason: "Aunque arriendas, puedes proteger tus bienes dentro del hogar."},

	// Accidentes Personales
	{ID: "r_acc_viaja", ProductID: pidAccidente, Name: "Se desplaza seguido", VariableKey: "travels_often",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Te desplazas con frecuencia: protégete frente a accidentes personales."},
	{ID: "r_acc_joven", ProductID: pidAccidente, Name: "Persona activa", VariableKey: "age",
		Operator: "lt", ExpectedValue: 40.0, Weight: 0.2, Active: true,
		Reason: "Por tu ritmo de vida activo, la cobertura de accidentes te da tranquilidad."},

	// Calificación financiera (usadas por FINANCIAL_QUALIFICATION).
	{ID: "r_vida_ingreso", ProductID: pidVida, Name: "Ingreso califica vida", VariableKey: "monthly_income",
		Operator: "gte", ExpectedValue: 2000000.0, Weight: 0.2, Active: true,
		Reason: "Con tus ingresos, el plan de vida familiar es sostenible sin apretar tu presupuesto."},
	{ID: "r_hogar_ahorro", ProductID: pidHogar, Name: "Ahorro cubre hogar", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 32000.0, Weight: 0.2, Active: true,
		Reason: "Tu capacidad de ahorro cubre la prima mensual del plan hogar."},
	{ID: "r_acc_ahorro", ProductID: pidAccidente, Name: "Ahorro cubre accidentes", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 18000.0, Weight: 0.1, Active: true,
		Reason: "La prima de accidentes personales entra en tu capacidad de ahorro."},
}

// ---------------------------------------------------------------------------
// Motor: operadores, flujo de preguntas y scoring
// ---------------------------------------------------------------------------

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// looseEqual compara con coerción tolerante (números por valor, resto por
// igualdad de string en minúscula) — así "propia"=="Propia" y 2==2.0.
func looseEqual(a, b interface{}) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok2 := toFloat(b); ok2 {
			return af == bf
		}
	}
	if ab, ok := a.(bool); ok {
		if bb, ok2 := b.(bool); ok2 {
			return ab == bb
		}
	}
	return strings.EqualFold(fmt.Sprint(a), fmt.Sprint(b))
}

// evalOp evalúa un ConditionOperator: usado por conditions de preguntas y por
// las reglas de recomendación (mismo motor que el real).
func evalOp(actual interface{}, op string, expected interface{}) bool {
	switch op {
	case "exists":
		return actual != nil
	case "equals":
		return looseEqual(actual, expected)
	case "not_equals":
		return !looseEqual(actual, expected)
	case "gt", "gte", "lt", "lte":
		a, ok1 := toFloat(actual)
		b, ok2 := toFloat(expected)
		if !ok1 || !ok2 {
			return false
		}
		switch op {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		case "lte":
			return a <= b
		}
	case "in":
		arr, ok := expected.([]interface{})
		if !ok {
			return false
		}
		for _, e := range arr {
			if looseEqual(actual, e) {
				return true
			}
		}
		return false
	case "contains":
		s, ok := actual.(string)
		sub, ok2 := expected.(string)
		return ok && ok2 && strings.Contains(strings.ToLower(s), strings.ToLower(sub))
	}
	return false
}

// conditionsMet: todas las conditions de la pregunta deben cumplirse (AND).
func conditionsMet(q Question, answers map[string]interface{}) bool {
	for _, c := range q.Conditions {
		if !evalOp(answers[c.DependsOnVariableKey], c.Operator, c.ExpectedValue) {
			return false
		}
	}
	return true
}

// nextQuestion devuelve la primera pregunta activa, no respondida y cuyas
// condiciones se cumplen, en orden de order_index. nil si no queda ninguna.
func nextQuestion(answers map[string]interface{}) *Question {
	ordered := make([]Question, len(questionsCatalog))
	copy(ordered, questionsCatalog)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OrderIndex < ordered[j].OrderIndex })
	for i := range ordered {
		q := ordered[i]
		if !q.Active {
			continue
		}
		if _, answered := answers[q.VariableKey]; answered {
			continue
		}
		if !conditionsMet(q, answers) {
			continue
		}
		return &q
	}
	return nil
}

func questionByID(id string) *Question {
	for i := range questionsCatalog {
		if questionsCatalog[i].ID == id {
			return &questionsCatalog[i]
		}
	}
	return nil
}

// recommend aplica el scoring ponderado: por producto suma los weights de las
// reglas activas que matchean; ordena por score desc y devuelve hasta `limit`.
// El reason mostrado es el de la regla de mayor peso que hizo match.
func recommend(answers map[string]interface{}, limit int) []Recommendation {
	type acc struct {
		score      float64
		bestWeight float64
		reason     string
	}
	byProduct := map[string]*acc{}
	for _, r := range rulesCatalog {
		if !r.Active {
			continue
		}
		if !evalOp(answers[r.VariableKey], r.Operator, r.ExpectedValue) {
			continue
		}
		a := byProduct[r.ProductID]
		if a == nil {
			a = &acc{}
			byProduct[r.ProductID] = a
		}
		a.score += r.Weight
		if r.Weight >= a.bestWeight {
			a.bestWeight = r.Weight
			a.reason = r.Reason
		}
	}

	var recs []Recommendation
	for pid, a := range byProduct {
		p := productByID(pid)
		if p == nil {
			continue
		}
		recs = append(recs, Recommendation{
			ProductID: pid, Code: p["code"].(string), Name: p["name"].(string),
			Category: p["category"].(string), BasePrice: p["base_price"].(float64),
			Score: round2(a.score), Reason: a.reason,
		})
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Score > recs[j].Score })
	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}
	return recs
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// validate replica la validación por field_type de la API real (422 re-pregunta).
func validate(q *Question, v interface{}) error {
	if q.Required && v == nil {
		return fmt.Errorf("value is required")
	}
	switch q.FieldType {
	case "number", "currency":
		if _, ok := toFloat(v); !ok {
			return fmt.Errorf("value must be a number")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("value must be a boolean")
		}
	case "select", "radio":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("value must be one of the options")
		}
		for _, o := range q.Options {
			if strings.EqualFold(s, fmt.Sprint(o)) {
				return nil
			}
		}
		return fmt.Errorf("value must be one of: %v", q.Options)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Store en memoria
// ---------------------------------------------------------------------------

type store struct {
	mu    sync.Mutex
	users map[string]*User
	convs map[string]*Conversation
	vars  map[string]map[string]UserVariable // user_id -> key -> variable
}

func newStore() *store {
	return &store{
		users: map[string]*User{},
		convs: map[string]*Conversation{},
		vars:  map[string]map[string]UserVariable{},
	}
}

// userAnswers junta el perfil completo del usuario para el motor de reglas:
// variables persistidas (PUT /variables, camino Guardian) + respuestas de sus
// conversaciones (camino answers rígido). Las variables mandan en conflicto.
func (s *store) userAnswers(userID string) map[string]interface{} {
	merged := map[string]interface{}{}
	for _, c := range s.convs {
		if c.UserID == userID {
			for k, v := range c.answers {
				merged[k] = v
			}
		}
	}
	for k, v := range s.vars[userID] {
		merged[k] = v.Value
	}
	return merged
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// applyNext fija next_question/current/status/can_generate según las respuestas.
func (c *Conversation) applyNext() {
	if q := nextQuestion(c.answers); q != nil {
		c.NextQuestion = q
		c.CurrentQuestionID = q.ID
		c.Status = "collecting_data"
		c.CanGenerateRecommendation = false
		return
	}
	c.NextQuestion = nil
	c.CurrentQuestionID = ""
	c.Status = "ready_for_recommendation"
	c.CanGenerateRecommendation = true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{"detail": msg})
}

func (s *store) handleProducts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, products)
}

// GET /api/v1/questions — cuestionario activo (inspección/debug, spec real).
func (s *store) handleQuestions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, questionsCatalog)
}

// GET /api/v1/rules — reglas del motor (inspección/debug, spec real).
func (s *store) handleRules(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("product_id")
	out := []Rule{}
	for _, rl := range rulesCatalog {
		if pid == "" || rl.ProductID == pid {
			out = append(out, rl)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *store) handleUserSearch(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	doc := r.URL.Query().Get("document_number")
	email := r.URL.Query().Get("email")
	out := []User{}
	s.mu.Lock()
	for _, u := range s.users {
		if (phone != "" && u.Phone == phone) ||
			(doc != "" && u.DocumentNumber == doc) ||
			(email != "" && u.Email == email) ||
			(phone == "" && doc == "" && email == "") {
			out = append(out, *u)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *store) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = uuid4()
	in.IsActive = true
	in.CreatedAt, in.UpdatedAt = now(), now()
	s.users[in.ID] = &in
	writeJSON(w, http.StatusCreated, in)
}

// PUT /api/v1/users/{user_id}/variables — upsert batch de variables de perfil
// (VariableValueInput[]). Devuelve el estado almacenado (UserVariableRead[]).
func (s *store) handleVariablesPut(w http.ResponseWriter, r *http.Request) {
	var in []UserVariable
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: expected VariableValueInput[]")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	uid := r.PathValue("user_id")
	if _, ok := s.users[uid]; !ok {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if s.vars[uid] == nil {
		s.vars[uid] = map[string]UserVariable{}
	}
	for _, v := range in {
		if v.Key == "" {
			writeErr(w, http.StatusUnprocessableEntity, "key is required")
			return
		}
		if v.Source == "" {
			v.Source = "api"
		}
		s.vars[uid][v.Key] = v
	}
	writeJSON(w, http.StatusOK, s.varsList(uid))
}

// GET /api/v1/users/{user_id}/variables
func (s *store) handleVariablesGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid := r.PathValue("user_id")
	if _, ok := s.users[uid]; !ok {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, s.varsList(uid))
}

func (s *store) varsList(uid string) []UserVariable {
	out := []UserVariable{}
	for _, v := range s.vars[uid] {
		out = append(out, v)
	}
	return out
}

// POST /api/v1/recommendations/users/{user_id}?limit= — scoring sobre el perfil
// COMPLETO del usuario (variables + respuestas), sin exigir el flujo answers.
func (s *store) handleUserRecommendations(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid := r.PathValue("user_id")
	if _, ok := s.users[uid]; !ok {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":         uid,
		"snapshot_id":     uuid4(),
		"recommendations": recommend(s.userAnswers(uid), limit),
	})
}

// GET /api/v1/users/{user_id}
func (s *store) handleUserGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[r.PathValue("user_id")]
	if !ok {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *store) handleConversationCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID            string `json:"user_id"`
		Channel           string `json:"channel"`
		ExternalSessionID string `json:"external_session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.UserID == "" {
		writeErr(w, http.StatusBadRequest, "user_id required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[in.UserID]; !ok {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	ch := in.Channel
	if ch == "" {
		ch = "whatsapp"
	}
	c := &Conversation{ID: uuid4(), UserID: in.UserID, Channel: ch, answers: map[string]interface{}{}}
	c.applyNext()
	s.convs[c.ID] = c
	writeJSON(w, http.StatusCreated, c)
}

// GET /api/v1/conversations/{id}
func (s *store) handleConversationGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *store) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QuestionID string      `json:"question_id"`
		Value      interface{} `json:"value"`
		Source     string      `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	if c.Status == "completed" || c.Status == "cancelled" {
		writeErr(w, http.StatusConflict, "conversation already "+c.Status)
		return
	}
	if c.NextQuestion == nil || in.QuestionID != c.NextQuestion.ID {
		writeErr(w, http.StatusConflict, "question_id does not match the current question")
		return
	}
	q := questionByID(in.QuestionID)
	if q == nil {
		writeErr(w, http.StatusNotFound, "question not found")
		return
	}
	if err := validate(q, in.Value); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Normaliza números a float64 para que el scoring compare por valor.
	val := in.Value
	if q.FieldType == "number" || q.FieldType == "currency" {
		if f, ok := toFloat(in.Value); ok {
			val = f
		}
	}
	c.answers[q.VariableKey] = val
	c.applyNext() // recalcula next_question (respeta branching por conditions)
	writeJSON(w, http.StatusOK, c)
}

func (s *store) handleComplete(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	if c.Status == "completed" {
		writeErr(w, http.StatusConflict, "conversation already completed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	c.Status = "completed"
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"conversation_id": c.ID,
		"status":          "completed",
		"snapshot_id":     uuid4(),
		"user_id":         c.UserID,
		"recommendations": recommend(c.answers, limit),
	})
}

func (s *store) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "mock-protege"})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

func main() {
	s := newStore()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/products", s.handleProducts)
	mux.HandleFunc("GET /api/v1/questions", s.handleQuestions)
	mux.HandleFunc("GET /api/v1/rules", s.handleRules)
	mux.HandleFunc("GET /api/v1/users/search", s.handleUserSearch)
	mux.HandleFunc("POST /api/v1/users", s.handleUserCreate)
	mux.HandleFunc("GET /api/v1/users/{user_id}", s.handleUserGet)
	mux.HandleFunc("PUT /api/v1/users/{user_id}/variables", s.handleVariablesPut)
	mux.HandleFunc("GET /api/v1/users/{user_id}/variables", s.handleVariablesGet)
	mux.HandleFunc("POST /api/v1/recommendations/users/{user_id}", s.handleUserRecommendations)
	mux.HandleFunc("POST /api/v1/conversations", s.handleConversationCreate)
	mux.HandleFunc("GET /api/v1/conversations/{id}", s.handleConversationGet)
	mux.HandleFunc("POST /api/v1/conversations/{id}/answers", s.handleAnswer)
	mux.HandleFunc("POST /api/v1/conversations/{id}/complete", s.handleComplete)

	addr := ":9000"
	log.Printf("mock-protege (Colsubsidio Protege API · demo) on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}
