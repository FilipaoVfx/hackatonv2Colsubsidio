package main

import (
	"encoding/csv"
	"hash/fnv"
	"log"
	"os"
	"strings"
)

// Afiliado 360 (recursos_seguros del reto): store en memoria de la muestra
// anonimizada de afiliados (demografía + marcas de consumo). El Guardian lo usa
// para PRE-CARGAR el perfil del cliente al abrir la conversación — el asesor no
// pregunta lo que Colsubsidio ya sabe (spec: evitar interrogatorio).
//
// La base no trae teléfono (anonimizada por SERIE): la vinculación
// teléfono→afiliado es DETERMINÍSTICA SIMULADA para la demo (hash del teléfono
// sobre la muestra). En producción vendría del maestro de afiliados. Se declara
// en 08_FEATURE_AFILIADO_360.md.
//
// Todo entra a la Protege API por su punto de extensión natural:
// PUT /users/{id}/variables con source "colsubsidio_360" — cero cambio de contrato.

type Affiliate struct {
	Serie         string
	Gender        string
	AgeRange      string
	SalaryRange   string
	Category      string
	FamilySegment string
	City          string
	Drugstore     bool // DROGUERIA — única marca con señal real (17.6% global)
	Hotels        bool
	Piscilago     bool
	Agencies      bool
	Housing       bool
}

type Affiliates struct {
	rows    []Affiliate
	bySerie map[string]int // SERIE -> índice en rows (lookup REAL)
}

// NewAffiliates loads the sample CSV (AFFILIATES_CSV or default). Missing file
// is not an error: the 360 enrichment simply stays off.
func NewAffiliates() *Affiliates {
	path := os.Getenv("AFFILIATES_CSV")
	if path == "" {
		path = "data/affiliates_sample.csv"
	}
	a := &Affiliates{}
	f, err := os.Open(path)
	if err != nil {
		log.Printf("afiliado360: sample not available (%v) — enrichment off", err)
		return a
	}
	defer f.Close()
	r := csv.NewReader(f)
	recs, err := r.ReadAll()
	if err != nil || len(recs) < 2 {
		log.Printf("afiliado360: csv unreadable (%v)", err)
		return a
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[strings.TrimSpace(h)] = i
	}
	get := func(row []string, key string) string {
		if i, ok := idx[key]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	si := func(row []string, key string) bool { return strings.EqualFold(get(row, key), "SI") }
	a.bySerie = make(map[string]int, len(recs)-1)
	for _, row := range recs[1:] {
		a.bySerie[get(row, "SERIE")] = len(a.rows)
		a.rows = append(a.rows, Affiliate{
			Serie:         get(row, "SERIE"),
			Gender:        get(row, "GENERO"),
			AgeRange:      get(row, "RANGO_EDAD"),
			SalaryRange:   get(row, "RANGO_SALARIAL"),
			Category:      get(row, "CATEGORIA"),
			FamilySegment: get(row, "SEGMENTO_GRUPO_FAMILIAR"),
			City:          get(row, "CIUDAD_AFILIADO"),
			Drugstore:     si(row, "DROGUERIA"),
			Hotels:        si(row, "HOTELES"),
			Piscilago:     si(row, "PISCILAGO"),
			Agencies:      si(row, "AGENCIAS"),
			Housing:       si(row, "VIVIENDA"),
		})
	}
	log.Printf("afiliado360: %d afiliados cargados (muestra anonimizada)", len(a.rows))
	return a
}

func (a *Affiliates) Enabled() bool { return a != nil && len(a.rows) > 0 }

// ForPhone returns the affiliate deterministically bound to a phone for the
// demo (same phone → same affiliate). Vinculación SIMULADA — usada solo como
// estimación inicial; BySerie la reemplaza cuando el cliente confirma su serie.
func (a *Affiliates) ForPhone(phone string) (Affiliate, bool) {
	if !a.Enabled() {
		return Affiliate{}, false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(canonPhone(phone)))
	return a.rows[int(h.Sum32())%len(a.rows)], true
}

// BySerie is the REAL master lookup: the customer states their affiliate
// number in the conversation and we fetch exactly that record.
func (a *Affiliates) BySerie(serie string) (Affiliate, bool) {
	if !a.Enabled() {
		return Affiliate{}, false
	}
	// tolera "serie 12345", "No. 12345", etc.: solo dígitos
	digits := strings.TrimPrefix(canonPhone(serie), "+")
	if i, ok := a.bySerie[digits]; ok {
		return a.rows[i], true
	}
	return Affiliate{}, false
}

// smlv is the Colombian monthly minimum wage used to estimate income from the
// salary range (valor 2026 aprox; solo estimación para el motor de reglas).
const smlv = 1_423_500.0

// incomeEstimate maps RANGO_SALARIAL to an estimated monthly income (midpoint
// of the range × SMLV). Empty when the range is unknown.
func incomeEstimate(salaryRange string) (float64, bool) {
	s := strings.ToLower(salaryRange)
	mid := map[string]float64{
		"menor al smlv":      0.75,
		"entre 1 y 1.5 smlv": 1.25, "entre 1.5 y 2 smlv": 1.75,
		"entre 2 y 2.5 smlv": 2.25, "entre 2.5 y 3 smlv": 2.75,
		"entre 3 y 4 smlv": 3.5, "entre 4 y 6 smlv": 5,
		"entre 6 y 8 smlv": 7, "entre 8 y 10 smlv": 9,
		"entre 10 y 20 smlv": 15, "mayor a 20 smlv": 25,
	}
	for k, m := range mid {
		if strings.Contains(s, k) {
			return m * smlv, true
		}
	}
	return 0, false
}

// Variables maps the affiliate to canonical Protege variables. La base se
// traduce al vocabulario de la API (nunca al revés): monthly_income estimado
// alimenta las reglas existentes; el resto son claves 360 nuevas.
//
// confirmed = el cliente dio su número de afiliado y este es SU registro del
// maestro. Sin confirmar (vinculación demo por hash de teléfono) el perfil es
// una estimación: entra con confianza baja y SIN monthly_income, porque ese
// dato calificaría la etapa financiera y dispararía las reglas de capacidad
// con un ingreso que nadie confirmó.
func (af Affiliate) Variables(confirmed bool) []VariableValue {
	conf := 0.85 // dato de la base, no confirmado en conversación
	if !confirmed {
		conf = 0.4 // estimación: por debajo del umbral de "hecho confirmado" (0.6)
	}
	v := func(key string, value interface{}) VariableValue {
		c := conf
		return VariableValue{Key: key, Value: value, Source: "colsubsidio_360", Confidence: &c}
	}
	out := []VariableValue{
		v("affiliate_serie", af.Serie),
		v("age_range", af.AgeRange),
		v("rango_salarial", af.SalaryRange),
		v("uses_drugstore", af.Drugstore),
	}
	if af.City != "" {
		out = append(out, v("city", af.City))
	}
	if af.Gender != "" {
		out = append(out, v("gender", af.Gender))
	}
	if af.FamilySegment != "" {
		out = append(out, v("family_segment", af.FamilySegment))
	}
	if income, ok := incomeEstimate(af.SalaryRange); ok && confirmed {
		out = append(out, v("monthly_income", income))
	}
	if af.Hotels {
		out = append(out, v("uses_hotels", true))
	}
	if af.Agencies {
		out = append(out, v("uses_travel_agency", true))
	}
	if af.Housing {
		out = append(out, v("uses_housing_services", true))
	}
	return out
}

// derivedRules son las reglas de afinidad DERIVADAS DE LA BASE (ETL 2026-07-24
// sobre 500.000 afiliados). Pesos modestos: complementan la conversación, no la
// dominan. Sin target de compra de seguros en la base, la relación
// consumo→seguro es afinidad heurística — declarado en el 08_FEATURE doc.
//
// Evidencia (ETL):
//   DROGUERIA global 17.6%; gradiente salarial 7%→35%; 20-35 años: 24%.
//   Marcas HOTELES/PISCILAGO/AGENCIAS/VIVIENDA ≈ 0% en la muestra (sin señal).
func derivedRules(products []ProtegeProduct) []ProtegeRule {
	find := func(cat string) string {
		for _, p := range products {
			if p.Category == cat {
				return p.ID
			}
		}
		return ""
	}
	vida, hogar, acc := find("vida"), find("hogar"), find("accidentes")
	var out []ProtegeRule
	add := func(pid, name, key, op string, expected interface{}, w float64, reason string) {
		if pid == "" {
			return
		}
		out = append(out, ProtegeRule{
			ProductID: pid, Name: name, VariableKey: key, Operator: op,
			ExpectedValue: expected, Weight: w, Reason: reason, Active: true,
		})
	}
	add(vida, "360 — usuario droguerías", "uses_drugstore", "equals", true, 0.3,
		"Usas las droguerías Colsubsidio: cuidar tu salud ya es parte de tu vida, y un seguro de vida la respalda (17.6% de los afiliados comparte este hábito).")
	add(acc, "360 — etapa activa", "age_range", "equals", "20 a 35 años", 0.15,
		"Estás en la etapa más activa (los afiliados de 20-35 años son los que más usan los servicios): la protección de accidentes te acompaña en ese ritmo.")
	add(hogar, "360 — capacidad desde 2 SMLV", "monthly_income", "gte", 2*smlv, 0.2,
		"Desde 2 SMLV el uso de servicios de protección crece del 12% al 31% entre afiliados: tu hogar puede asegurarse sin apretar el presupuesto.")
	add(vida, "360 — Bogotá", "city", "equals", "BOGOTA D.C.", 0.1,
		"En Bogotá los afiliados usan más los servicios de salud (21% vs 16% nacional): el seguro de vida es el complemento natural.")
	return out
}
