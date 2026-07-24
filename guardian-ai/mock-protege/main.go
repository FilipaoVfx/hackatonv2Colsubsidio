// mock-protege es un mock local de la Colsubsidio Protege API (OpenAPI 0.1.0).
//
// Para efectos de la demo se usa un mock de la API de integración debido a
// restricciones de acceso de red sobre la API real (147.93.11.136:9000 filtra
// nuestra IP de egress). Responde con el MISMO contrato (rutas, schemas y
// códigos de estado) que la API real, así que el resto del sistema — el
// backend Go (colsubsidio.go / protege.go), el canal WhatsApp y el Pipeline —
// no sabe que es un mock: basta apuntar COLSUBSIDIO_API_URL a este servicio.
//
// El catálogo de /api/v1/products son los datos REALES capturados de la API
// el 2026-07-24. El flujo de preguntas y las reglas de recomendación son una
// réplica razonable del motor (questions dinámicas por variables + reglas).
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
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Schemas (subset fiel del OpenAPI 0.1.0 que consume el backend)
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
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type Question struct {
	ID          string        `json:"id"`
	VariableKey string        `json:"variable_key"`
	Text        string        `json:"text"`
	FieldType   string        `json:"field_type"` // text|number|currency|boolean|radio|select|multi_select|date|email|phone
	Required    bool          `json:"required"`
	HelpText    string        `json:"help_text"`
	Placeholder string        `json:"placeholder"`
	Options     []interface{} `json:"options"`
}

type Conversation struct {
	ID                        string    `json:"id"`
	UserID                    string    `json:"user_id"`
	Channel                   string    `json:"channel"`
	Status                    string    `json:"status"` // new|collecting_data|ready_for_recommendation|completed|cancelled
	CurrentQuestionID         string    `json:"current_question_id"`
	NextQuestion              *Question `json:"next_question"`
	CanGenerateRecommendation bool      `json:"can_generate_recommendation"`

	answers map[string]interface{} // variable_key -> valor respondido
}

type Recommendation struct {
	ProductID string  `json:"product_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	BasePrice float64 `json:"base_price"`
	Reason    string  `json:"reason"`
}

// ---------------------------------------------------------------------------
// Catálogo: datos REALES capturados de GET /api/v1/products (2026-07-24)
// ---------------------------------------------------------------------------

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
		"id": "0e92218c-3d75-4024-aed2-7ea1cc5f0950",
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
		"id": "a49e8875-f8d4-43f3-a9f0-9a144b41a0ff",
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
		"id": "759f18ae-a795-449a-b376-c7a7ba6111d4",
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
		"id": "71d3c72e-47ab-4941-8223-73e14bfa82b9",
		"created_at": "2026-07-24T02:20:42.767413Z", "updated_at": "2026-07-24T02:20:42.767413Z",
	},
}

// ---------------------------------------------------------------------------
// Flujo de preguntas dinámicas (réplica del motor) + reglas de recomendación
// ---------------------------------------------------------------------------

// questionFlow es el cuestionario que el mock sirve en orden. Cada pregunta
// alimenta una variable de perfil; las reglas de abajo mapean variables →
// producto del catálogo.
var questionFlow = []Question{
	{ID: "q_name", VariableKey: "full_name", Text: "¿Cuál es tu nombre completo?",
		FieldType: "text", Required: true, Placeholder: "Ej: Ana María Rojas"},
	{ID: "q_age", VariableKey: "age", Text: "¿Qué edad tienes?",
		FieldType: "number", Required: true, HelpText: "En años cumplidos", Placeholder: "Ej: 34"},
	{ID: "q_dependents", VariableKey: "has_dependents", Text: "¿Tienes hijos o dependientes económicos?",
		FieldType: "boolean", Required: true},
	{ID: "q_pet", VariableKey: "has_pet", Text: "¿Tienes perro o gato en casa?",
		FieldType: "boolean", Required: true},
	{ID: "q_housing", VariableKey: "housing_type", Text: "¿Vives en casa propia, arrendada o familiar?",
		FieldType: "select", Required: true, Options: []interface{}{"propia", "arrendada", "familiar"}},
	{ID: "q_travel", VariableKey: "travels_often", Text: "¿Te desplazas con frecuencia por trabajo o estudio?",
		FieldType: "boolean", Required: true},
}

// recommend aplica las reglas del motor sobre las respuestas y devuelve hasta
// `limit` recomendaciones del catálogo real.
func recommend(answers map[string]interface{}, limit int) []Recommendation {
	var recs []Recommendation
	add := func(code, reason string) {
		for _, p := range products {
			if p["code"] == code {
				recs = append(recs, Recommendation{
					ProductID: p["id"].(string), Code: code, Name: p["name"].(string),
					Category: p["category"].(string), BasePrice: p["base_price"].(float64), Reason: reason,
				})
				return
			}
		}
	}
	if v, _ := answers["has_dependents"].(bool); v {
		add("VIDA_FAMILIAR_TEST", "Tienes dependientes económicos: este seguro les da respaldo ante eventos inesperados.")
	}
	if v, _ := answers["has_pet"].(bool); v {
		add("MASCOTA_PROTEGIDA_TEST", "Tienes mascota en casa: cubre el cuidado de tu perro o gato.")
	}
	if h, _ := answers["housing_type"].(string); h == "propia" || h == "arrendada" {
		add("HOGAR_PROTEGIDO_TEST", "Vives en casa "+h+": protege tu vivienda y los bienes contenidos en ella.")
	}
	if v, _ := answers["travels_often"].(bool); v {
		add("ACCIDENTES_PERSONALES_TEST", "Te desplazas con frecuencia: protégete frente a accidentes personales.")
	}
	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}
	return recs
}

// ---------------------------------------------------------------------------
// Store en memoria
// ---------------------------------------------------------------------------

type store struct {
	mu    sync.Mutex
	users map[string]*User         // id -> user
	convs map[string]*Conversation // id -> conversation
}

func newStore() *store {
	return &store{users: map[string]*User{}, convs: map[string]*Conversation{}}
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

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

// GET /api/v1/products — catálogo completo (tal cual la API real).
func (s *store) handleProducts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, products)
}

// GET /api/v1/users/search?phone= — lista de usuarios que coinciden (vacía si ninguno).
func (s *store) handleUserSearch(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	out := []User{}
	s.mu.Lock()
	for _, u := range s.users {
		if phone == "" || u.Phone == phone {
			out = append(out, *u)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// POST /api/v1/users — crea un usuario.
func (s *store) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = uuid4()
	in.CreatedAt, in.UpdatedAt = now(), now()
	s.users[in.ID] = &in
	writeJSON(w, http.StatusCreated, in)
}

// POST /api/v1/conversations — abre conversación y devuelve la primera pregunta.
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
	c := &Conversation{
		ID: uuid4(), UserID: in.UserID, Channel: ch, Status: "collecting_data",
		answers: map[string]interface{}{},
	}
	c.NextQuestion = &questionFlow[0]
	c.CurrentQuestionID = questionFlow[0].ID
	s.convs[c.ID] = c
	writeJSON(w, http.StatusCreated, c)
}

// POST /api/v1/conversations/{id}/answers — responde la pregunta actual y avanza.
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
	q := c.NextQuestion
	if err := validate(q, in.Value); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	c.answers[q.VariableKey] = in.Value

	// avanzar a la siguiente pregunta o marcar listo para recomendación
	idx := -1
	for i := range questionFlow {
		if questionFlow[i].ID == q.ID {
			idx = i
			break
		}
	}
	if idx+1 < len(questionFlow) {
		c.NextQuestion = &questionFlow[idx+1]
		c.CurrentQuestionID = questionFlow[idx+1].ID
	} else {
		c.NextQuestion = nil
		c.CurrentQuestionID = ""
		c.Status = "ready_for_recommendation"
		c.CanGenerateRecommendation = true
	}
	writeJSON(w, http.StatusOK, c)
}

// validate replica la validación por field_type de la API real (422 re-pregunta).
func validate(q *Question, v interface{}) error {
	if q.Required && v == nil {
		return fmt.Errorf("value is required")
	}
	switch q.FieldType {
	case "number", "currency":
		if _, ok := v.(float64); !ok {
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

// POST /api/v1/conversations/{id}/complete?limit= — cierra y genera recomendaciones.
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

// GET /api/v1/health — conveniencia para la demo (no hace parte del spec).
func (s *store) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "mock-protege"})
}

// logRequests deja traza de cada llamada (visible con docker logs).
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
	mux.HandleFunc("GET /api/v1/users/search", s.handleUserSearch)
	mux.HandleFunc("POST /api/v1/users", s.handleUserCreate)
	mux.HandleFunc("POST /api/v1/conversations", s.handleConversationCreate)
	mux.HandleFunc("POST /api/v1/conversations/{id}/answers", s.handleAnswer)
	mux.HandleFunc("POST /api/v1/conversations/{id}/complete", s.handleComplete)

	addr := ":9000"
	log.Printf("mock-protege (Colsubsidio Protege API · demo) on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}
