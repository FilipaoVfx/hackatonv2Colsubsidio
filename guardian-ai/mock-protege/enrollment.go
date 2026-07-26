// enrollment.go — cotización y vinculación (el cierre de la venta).
//
// La API real de Protege (OpenAPI 0.1.0) llega hasta la recomendación: no
// publica endpoints de compra. Estos tres recursos son una EXTENSIÓN del mock,
// añadida para que el flujo de la demo termine con la persona asegurada y no en
// "un asesor te contactará". Están marcados como extensión en GET /api/v1/health
// y en la documentación, para no dar a entender que Colsubsidio los expone hoy.
//
//	POST /api/v1/quotes                    cotiza producto + coberturas elegidas
//	POST /api/v1/enrollments               acepta la cotización y emite solicitud
//	GET  /api/v1/enrollments/{id}          consulta la solicitud
//	GET  /api/v1/users/{user_id}/enrollments  solicitudes de una persona
package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	crand "crypto/rand"
)

// Quote es una cotización: producto + coberturas seleccionadas + prima mensual
// resultante. Se guarda para que la vinculación se haga contra un precio ya
// mostrado al cliente y no contra uno recalculado a espaldas suyas.
type Quote struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	ProductID    string     `json:"product_id"`
	ProductName  string     `json:"product_name"`
	BasePrice    float64    `json:"base_price"`
	Coverages    []Coverage `json:"coverages"`     // TODAS, con Included ya resuelto
	MonthlyPrice float64    `json:"monthly_price"` // base + deltas de las opcionales elegidas
	Currency     string     `json:"currency"`
	CreatedAt    string     `json:"created_at"`
}

// Enrollment es la solicitud de vinculación emitida tras la aceptación.
type Enrollment struct {
	ID                string     `json:"id"`
	ApplicationNumber string     `json:"application_number"`
	UserID            string     `json:"user_id"`
	QuoteID           string     `json:"quote_id"`
	ProductID         string     `json:"product_id"`
	ProductName       string     `json:"product_name"`
	MonthlyPrice      float64    `json:"monthly_price"`
	Currency          string     `json:"currency"`
	Coverages         []Coverage `json:"coverages"`
	Status            string     `json:"status"` // CONFIRMED
	Summary           string     `json:"summary"`
	NextStepURL       string     `json:"next_step_url"`
	CreatedAt         string     `json:"created_at"`
}

// productCoverages devuelve las coberturas declaradas del producto (catalog.go).
func productCoverages(p map[string]interface{}) []Coverage {
	if cs, ok := p["coverages"].([]Coverage); ok {
		return cs
	}
	return nil
}

// priceProduct resuelve la prima: base + delta de cada cobertura OPCIONAL que el
// cliente eligió. Devuelve la lista completa de coberturas con Included ya
// aplicado, para que el resumen muestre exactamente lo que se contrató.
// Una clave desconocida es un error explícito: nunca se cobra en silencio.
func priceProduct(p map[string]interface{}, selected []string) (float64, []Coverage, error) {
	all := productCoverages(p)
	want := map[string]bool{}
	for _, k := range selected {
		want[strings.TrimSpace(k)] = true
	}
	price, _ := p["base_price"].(float64)
	out := make([]Coverage, 0, len(all))
	for _, c := range all {
		if c.Included {
			out = append(out, c)
			delete(want, c.Key)
			continue
		}
		if want[c.Key] {
			c.Included = true
			price += c.PriceDelta
			delete(want, c.Key)
		}
		out = append(out, c)
	}
	for k := range want {
		return 0, nil, fmt.Errorf("unknown coverage: %s", k)
	}
	return price, out, nil
}

// acquisitionURL es el enlace real de Colsubsidio para ese producto (primera
// fuente citada en sourceapi.json); si el producto no trae fuentes, cae al
// portafolio de seguros familiares.
func acquisitionURL(p map[string]interface{}) string {
	meta, _ := p["metadata_json"].(map[string]interface{})
	if meta != nil {
		if srcs, ok := meta["fuentes"].([]string); ok && len(srcs) > 0 {
			return srcs[0]
		}
	}
	return "https://www.colsubsidio.com/seguros/familiares"
}

// applicationNumber genera el radicado visible para el cliente.
func applicationNumber() string {
	n, err := crand.Int(crand.Reader, big.NewInt(1000000))
	if err != nil {
		return "COL-2026-000000"
	}
	return fmt.Sprintf("COL-2026-%06d", n.Int64())
}

// summaryOf redacta el resumen que el cliente recibe y queda archivado con la
// solicitud: qué contrató, qué cubre y cuánto paga.
func summaryOf(e *Enrollment) string {
	var inc []string
	for _, c := range e.Coverages {
		if c.Included {
			inc = append(inc, c.Label)
		}
	}
	return fmt.Sprintf(
		"Solicitud %s: %s por $%.0f COP al mes. Coberturas contratadas: %s.",
		e.ApplicationNumber, e.ProductName, e.MonthlyPrice, strings.Join(inc, ", "),
	)
}

// --- handlers ---------------------------------------------------------------

type quoteRequest struct {
	UserID    string   `json:"user_id"`
	ProductID string   `json:"product_id"`
	Coverages []string `json:"coverages"`
}

func (s *store) handleQuoteCreate(w http.ResponseWriter, r *http.Request) {
	var in quoteRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	p := productByID(in.ProductID)
	if p == nil {
		writeErr(w, http.StatusNotFound, "product not found")
		return
	}
	price, covs, err := priceProduct(p, in.Coverages)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	q := &Quote{
		ID: uuid4(), UserID: in.UserID, ProductID: in.ProductID,
		ProductName: p["name"].(string), BasePrice: p["base_price"].(float64),
		Coverages: covs, MonthlyPrice: price, Currency: "COP", CreatedAt: now(),
	}
	s.mu.Lock()
	s.quotes[q.ID] = q
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, q)
}

type enrollRequest struct {
	UserID    string   `json:"user_id"`
	QuoteID   string   `json:"quote_id"`
	ProductID string   `json:"product_id"` // alterno: vincular sin cotizar antes
	Coverages []string `json:"coverages"`
	Accepted  bool     `json:"accepted"`
}

func (s *store) handleEnrollmentCreate(w http.ResponseWriter, r *http.Request) {
	var in enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	// Sin aceptación explícita no hay vinculación: el cierre lo autoriza la
	// persona, no el agente.
	if !in.Accepted {
		writeErr(w, http.StatusUnprocessableEntity, "accepted must be true")
		return
	}

	var (
		q     *Quote
		price float64
		covs  []Coverage
		p     map[string]interface{}
	)
	if in.QuoteID != "" {
		s.mu.Lock()
		q = s.quotes[in.QuoteID]
		s.mu.Unlock()
		if q == nil {
			writeErr(w, http.StatusNotFound, "quote not found")
			return
		}
		p = productByID(q.ProductID)
		price, covs = q.MonthlyPrice, q.Coverages
	} else {
		p = productByID(in.ProductID)
		if p == nil {
			writeErr(w, http.StatusNotFound, "product not found")
			return
		}
		var err error
		price, covs, err = priceProduct(p, in.Coverages)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "product not found")
		return
	}

	uid := in.UserID
	if uid == "" && q != nil {
		uid = q.UserID
	}
	e := &Enrollment{
		ID: uuid4(), ApplicationNumber: applicationNumber(), UserID: uid,
		ProductID: p["id"].(string), ProductName: p["name"].(string),
		MonthlyPrice: price, Currency: "COP", Coverages: covs,
		Status: "CONFIRMED", NextStepURL: acquisitionURL(p), CreatedAt: now(),
	}
	if q != nil {
		e.QuoteID = q.ID
	}
	e.Summary = summaryOf(e)

	s.mu.Lock()
	s.enrollments[e.ID] = e
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, e)
}

func (s *store) handleEnrollmentGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	e := s.enrollments[r.PathValue("id")]
	s.mu.Unlock()
	if e == nil {
		writeErr(w, http.StatusNotFound, "enrollment not found")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *store) handleUserEnrollments(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("user_id")
	s.mu.Lock()
	out := []*Enrollment{}
	for _, e := range s.enrollments {
		if e.UserID == uid {
			out = append(out, e)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}
