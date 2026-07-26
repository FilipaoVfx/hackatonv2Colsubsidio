package main

import (
	"context"
	"fmt"
	"strings"
)

// closing.go — cotización y vinculación: el tramo que convierte una
// recomendación en una persona asegurada.
//
// HONESTIDAD: la API Protege real (OpenAPI 0.1.0) llega hasta la recomendación
// y NO publica endpoints de compra. `POST /api/v1/quotes` y
// `POST /api/v1/enrollments` son una extensión del mock (mock-protege/
// enrollment.go), declarada como tal en su GET /health. Cuando Colsubsidio
// exponga su propio cierre, se cambia la URL y el motor no se entera.
//
// Regla que NO cambia (RN-001/RN-005): las decisiones de negocio siguen siendo
// de la API. El motor elige el producto y las coberturas por reglas
// DETERMINISTAS sobre el ranking y el texto del cliente; el LLM nunca inventa
// un precio, una cobertura ni un radicado.

// ProtegeCoverage es una cobertura del producto tal como la publica la API.
type ProtegeCoverage struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Included   bool    `json:"included"`
	PriceDelta float64 `json:"price_delta"`
	Source     string  `json:"source"`
}

// ProtegeQuote es la cotización devuelta por la API.
type ProtegeQuote struct {
	ID           string            `json:"id"`
	ProductID    string            `json:"product_id"`
	ProductName  string            `json:"product_name"`
	BasePrice    float64           `json:"base_price"`
	MonthlyPrice float64           `json:"monthly_price"`
	Currency     string            `json:"currency"`
	Coverages    []ProtegeCoverage `json:"coverages"`
}

// ProtegeEnrollment es la solicitud de vinculación emitida por la API.
type ProtegeEnrollment struct {
	ID                string            `json:"id"`
	ApplicationNumber string            `json:"application_number"`
	ProductID         string            `json:"product_id"`
	ProductName       string            `json:"product_name"`
	MonthlyPrice      float64           `json:"monthly_price"`
	Currency          string            `json:"currency"`
	Coverages         []ProtegeCoverage `json:"coverages"`
	Status            string            `json:"status"`
	Summary           string            `json:"summary"`
	NextStepURL       string            `json:"next_step_url"`
}

// CreateQuote cotiza un producto con las coberturas opcionales elegidas.
func (c *ColsubsidioClient) CreateQuote(ctx context.Context, userID, productID string, coverages []string) (*ProtegeQuote, error) {
	if coverages == nil {
		coverages = []string{}
	}
	body := map[string]interface{}{"user_id": userID, "product_id": productID, "coverages": coverages}
	var out ProtegeQuote
	if err := c.do(ctx, "POST", "/api/v1/quotes", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEnrollment emite la solicitud de vinculación sobre una cotización ya
// mostrada al cliente. `accepted` viaja explícito: la API rechaza (422) todo lo
// que no lleve aceptación, así que el cierre nunca puede ocurrir sin un sí.
func (c *ColsubsidioClient) CreateEnrollment(ctx context.Context, userID, quoteID string) (*ProtegeEnrollment, error) {
	body := map[string]interface{}{"user_id": userID, "quote_id": quoteID, "accepted": true}
	var out ProtegeEnrollment
	if err := c.do(ctx, "POST", "/api/v1/enrollments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Selección determinista: qué producto y qué coberturas
// ---------------------------------------------------------------------------

// recOption es una recomendación con la estructura que el cierre necesita:
// el ranking lo sigue calculando la API, aquí solo se conserva.
type recOption struct {
	ProductID string
	Name      string
	Reason    string
	BasePrice float64
	Coverages []ProtegeCoverage
}

// Line renderiza la opción como se muestra en el chat.
func (o recOption) Line() string {
	if o.Reason == "" {
		return o.Name
	}
	return o.Name + " — " + o.Reason
}

// Optional devuelve las coberturas que el cliente puede añadir.
func (o recOption) Optional() []ProtegeCoverage {
	var out []ProtegeCoverage
	for _, c := range o.Coverages {
		if !c.Included {
			out = append(out, c)
		}
	}
	return out
}

// parseRecommendation convierte una recomendación cruda de la API en recOption.
func parseRecommendation(raw interface{}) (recOption, bool) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return recOption{}, false
	}
	name, reason := recFields(raw)
	if name == "" {
		return recOption{}, false
	}
	o := recOption{Name: name, Reason: reason}
	o.ProductID, _ = m["product_id"].(string)
	o.BasePrice, _ = m["base_price"].(float64)
	if arr, ok := m["coverages"].([]interface{}); ok {
		for _, c := range arr {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			cov := ProtegeCoverage{}
			cov.Key, _ = cm["key"].(string)
			cov.Label, _ = cm["label"].(string)
			cov.Included, _ = cm["included"].(bool)
			cov.PriceDelta, _ = cm["price_delta"].(float64)
			cov.Source, _ = cm["source"].(string)
			if cov.Key != "" {
				o.Coverages = append(o.Coverages, cov)
			}
		}
	}
	return o, true
}

// pickOption elige, sobre el texto del cliente, cuál de las opciones quiere.
// Por defecto la primera (la de mayor score de la API). Si el mensaje nombra
// otra de forma inequívoca, esa. La decisión es del motor y es explicable: no
// se le pide al LLM que "adivine" el producto.
func pickOption(options []recOption, text string) int {
	if len(options) == 0 {
		return -1
	}
	t := deaccent(text)
	best, bestLen := 0, 0
	for i, o := range options {
		for _, token := range significantWords(o.Name) {
			if strings.Contains(t, token) && len(token) > bestLen {
				best, bestLen = i, len(token)
			}
		}
	}
	return best
}

// catalogStopWords son palabras demasiado comunes en el catálogo para
// identificar un producto ("seguro", "de", "todo riesgo"...): si contaran,
// cualquier frase con "seguro" cambiaría la elección del cliente. Van sin
// tildes porque se comparan contra texto ya normalizado por deaccent.
var catalogStopWords = map[string]bool{
	"seguro": true, "seguros": true, "de": true, "del": true, "la": true, "el": true,
	"y": true, "en": true, "por": true, "para": true, "todo": true, "riesgo": true,
	"poliza": true, "asistencia": true, "positivo": true,
}

// Se trabaja SIN tildes: el cliente escribe "medica" y el catálogo dice
// "médica"; comparar crudo perdía la coincidencia.
func significantWords(name string) []string {
	var out []string
	for _, w := range strings.Fields(deaccent(name)) {
		w = strings.Trim(w, ".,;:()")
		if len(w) >= 4 && !catalogStopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// comparisonWords son las formas en que la gente pide comparar. Pedirlo y
// recibir una cotización del primero era responder otra cosa.
// Se comparan SIN tildes (ver deaccent): "compárame" no contiene "compar", y
// esa sola tilde hacía que el agente cotizara en vez de comparar.
var comparisonWords = []string{
	"compar", "diferencia", "cual me conviene", "cual es mejor",
	"cual conviene", "versus", " vs ", "se diferencian", "pros y contras",
}

// deaccent quita las tildes del español para comparar texto libre del cliente.
var accents = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
)

func deaccent(s string) string { return accents.Replace(strings.ToLower(s)) }

// wantsComparison reporta si el cliente pidió comparar en vez de elegir.
func wantsComparison(text string) bool {
	t := deaccent(text)
	for _, w := range comparisonWords {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// comparisonMessage pone las opciones lado a lado: precio, qué incluye y por
// qué la recomendó el sistema. Todo sale de la API; no se compara nada que el
// catálogo no haya publicado.
func comparisonMessage(options []recOption) string {
	lines := []string{"Te las comparo:"}
	for _, o := range options {
		lines = append(lines, "", fmt.Sprintf("*%s* — %s/mes", o.Name, money(o.BasePrice)))
		var inc []string
		for _, c := range o.Coverages {
			if c.Included {
				inc = append(inc, c.Label)
			}
		}
		if len(inc) > 0 {
			lines = append(lines, "Incluye: "+strings.Join(inc, ", "))
		}
		if n := len(o.Optional()); n > 0 {
			lines = append(lines, fmt.Sprintf("Opcionales que puedes añadir: %d", n))
		}
		if o.Reason != "" {
			lines = append(lines, "Por qué a ti: "+o.Reason)
		}
	}
	lines = append(lines, "", "Dime con cuál sigo y te la dejo lista, o pídeme añadir coberturas. 🛡️")
	return strings.Join(lines, "\n")
}

// pickCoverages detecta qué coberturas OPCIONALES pidió el cliente en su
// mensaje, comparando contra las etiquetas que la API publicó. Solo se cobra lo
// que el cliente nombró y la API declaró: nada inventado, nada silencioso.
func pickCoverages(opt recOption, text string) []string {
	t := deaccent(text)
	var keys []string
	for _, c := range opt.Optional() {
		if coverageMentioned(t, c) {
			keys = append(keys, c.Key)
		}
	}
	return keys
}

func coverageMentioned(text string, c ProtegeCoverage) bool {
	if strings.Contains(text, deaccent(c.Key)) {
		return true
	}
	words := significantWords(c.Label)
	if len(words) == 0 {
		return false
	}
	// Se exige que TODAS las palabras significativas de la etiqueta aparezcan:
	// con una sola ("médica") se colaban coberturas que el cliente no pidió.
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Redacción de los mensajes del cierre (deterministas, sin LLM)
// ---------------------------------------------------------------------------

func money(v float64) string { return fmt.Sprintf("$%s", thousands(int64(v+0.5))) }

func thousands(n int64) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, parts...), ".")
}

// quoteMessage es el resumen que el cliente ve ANTES de confirmar: producto,
// coberturas incluidas, lo que añadió, el precio final y qué más puede añadir.
func quoteMessage(q *ProtegeQuote) string {
	var inc, opt []string
	for _, c := range q.Coverages {
		if c.Included {
			inc = append(inc, "• "+c.Label)
		} else {
			opt = append(opt, fmt.Sprintf("• %s (+%s/mes)", c.Label, money(c.PriceDelta)))
		}
	}
	lines := []string{
		fmt.Sprintf("Te armé el plan *%s* por *%s al mes*:", q.ProductName, money(q.MonthlyPrice)),
		"",
		"Incluye:",
	}
	lines = append(lines, inc...)
	if len(opt) > 0 {
		lines = append(lines, "", "Puedes añadir:")
		lines = append(lines, opt...)
	}
	lines = append(lines, "", "¿Confirmas la vinculación con estas coberturas? Responde *sí* y te emito la solicitud. 🛡️")
	return strings.Join(lines, "\n")
}

// enrollmentMessage es la confirmación final: radicado, resumen y el enlace
// para el último paso de adquisición.
func enrollmentMessage(e *ProtegeEnrollment) string {
	var inc []string
	for _, c := range e.Coverages {
		if c.Included {
			inc = append(inc, "• "+c.Label)
		}
	}
	lines := []string{
		fmt.Sprintf("¡Listo! Quedas asegurado con *%s* ✅", e.ProductName),
		"",
		fmt.Sprintf("Radicado: *%s*", e.ApplicationNumber),
		fmt.Sprintf("Valor: *%s al mes*", money(e.MonthlyPrice)),
		"",
		"Coberturas contratadas:",
	}
	lines = append(lines, inc...)
	if e.NextStepURL != "" {
		lines = append(lines, "", "Último paso, formaliza el pago aquí: "+e.NextStepURL)
	}
	lines = append(lines, "", "Guarda tu radicado. Cualquier cosa, escríbeme por aquí 🙌")
	return strings.Join(lines, "\n")
}
