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
	// Coverages viaja con la recomendación para que el agente pueda explicar
	// qué incluye y qué se puede añadir SIN inventar coberturas.
	Coverages []Coverage `json:"coverages"`
}

// ---------------------------------------------------------------------------
// Catálogo: `products` y `productIDs` viven en catalog.go, GENERADO desde
// sourceapi.json (portafolio real publicado de Colsubsidio, 21 productos).
// Se regenera con scripts/gen_catalog.py; no se edita a mano. Las primas son
// estimaciones de demo y cada producto lo declara en metadata_json.price_source.
// ---------------------------------------------------------------------------

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

	// La edad afina varias reglas (cáncer, exequial, bicicletas) pero NO es
	// obligatoria: exigirla dejaba la conversación atascada cuando la persona
	// contaba todo lo demás y nunca su edad. Si aparece, las reglas la usan.
	{ID: "q_age", VariableKey: "age", Text: "¿Qué edad tienes?",
		FieldType: "number", Required: false, OrderIndex: 2, Active: true,
		HelpText: "En años cumplidos", Placeholder: "Ej: 34",
		Validation: map[string]interface{}{"min": 18.0, "max": 100.0}, Options: []interface{}{}, Conditions: []Condition{}},

	// Estado civil: junto con los dependientes es lo que separa el perfil
	// "soltero sin hijos" del perfil "casado con tres hijos".
	{ID: "q_marital", VariableKey: "marital_status", Text: "¿Cuál es tu estado civil?",
		FieldType: "select", Required: true, OrderIndex: 3, Active: true,
		Validation: map[string]interface{}{},
		Options:    []interface{}{"soltero", "casado", "union_libre", "separado", "viudo"}, Conditions: []Condition{}},

	{ID: "q_dependents", VariableKey: "has_dependents", Text: "¿Tienes hijos o dependientes económicos?",
		FieldType: "boolean", Required: true, OrderIndex: 4, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si has_dependents == true (branching real).
	{ID: "q_num_dependents", VariableKey: "num_dependents", Text: "¿Cuántas personas dependen económicamente de ti?",
		FieldType: "number", Required: true, OrderIndex: 5, Active: true,
		HelpText: "Número de dependientes", Placeholder: "Ej: 2",
		Validation: map[string]interface{}{"min": 1.0, "max": 15.0}, Options: []interface{}{},
		Conditions: []Condition{{DependsOnVariableKey: "has_dependents", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_pet", VariableKey: "has_pet", Text: "¿Tienes perro o gato en casa?",
		FieldType: "boolean", Required: false, OrderIndex: 6, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si has_pet == true.
	{ID: "q_pet_type", VariableKey: "pet_type", Text: "¿Es perro o gato?",
		FieldType: "select", Required: false, OrderIndex: 7, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{"perro", "gato"},
		Conditions: []Condition{{DependsOnVariableKey: "has_pet", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_housing", VariableKey: "housing_type", Text: "¿Vives en casa propia, arrendada o familiar?",
		FieldType: "select", Required: true, OrderIndex: 8, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{"propia", "arrendada", "familiar"}, Conditions: []Condition{}},

	{ID: "q_vehicle", VariableKey: "has_vehicle", Text: "¿Tienes carro o moto?",
		FieldType: "boolean", Required: false, OrderIndex: 9, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si has_vehicle == true.
	{ID: "q_vehicle_type", VariableKey: "vehicle_type", Text: "¿Carro o moto?",
		FieldType: "select", Required: false, OrderIndex: 10, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{"carro", "moto"},
		Conditions: []Condition{{DependsOnVariableKey: "has_vehicle", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_bike", VariableKey: "rides_bike", Text: "¿Te mueves en bicicleta?",
		FieldType: "boolean", Required: false, OrderIndex: 11, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	{ID: "q_travel", VariableKey: "travels_often", Text: "¿Te desplazas con frecuencia por trabajo o estudio?",
		FieldType: "boolean", Required: false, OrderIndex: 12, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Condicional: solo si travels_often == true.
	{ID: "q_travel_abroad", VariableKey: "travels_abroad", Text: "¿Esos viajes incluyen salidas del país?",
		FieldType: "boolean", Required: false, OrderIndex: 13, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{},
		Conditions: []Condition{{DependsOnVariableKey: "travels_often", Operator: "equals", ExpectedValue: true}}},

	{ID: "q_credit", VariableKey: "has_credit", Text: "¿Tienes algún crédito vigente (vivienda, vehículo o libre inversión)?",
		FieldType: "boolean", Required: false, OrderIndex: 14, Active: true,
		Validation: map[string]interface{}{}, Options: []interface{}{}, Conditions: []Condition{}},

	// Calificación financiera (FINANCIAL_QUALIFICATION del Guardian engine).
	{ID: "q_income", VariableKey: "monthly_income", Text: "¿Cuáles son tus ingresos mensuales aproximados?",
		FieldType: "currency", Required: true, OrderIndex: 20, Active: true,
		HelpText: "En pesos colombianos", Placeholder: "Ej: 2.500.000",
		Validation: map[string]interface{}{"min": 0.0}, Options: []interface{}{}, Conditions: []Condition{}},

	{ID: "q_savings", VariableKey: "saving_capacity", Text: "¿Cuánto podrías destinar al mes a tu protección?",
		FieldType: "currency", Required: true, OrderIndex: 21, Active: true,
		HelpText: "Capacidad de ahorro mensual", Placeholder: "Ej: 100.000",
		Validation: map[string]interface{}{"min": 0.0}, Options: []interface{}{}, Conditions: []Condition{}},
}

// ---------------------------------------------------------------------------
// Reglas de recomendación (schema fiel; scoring ponderado)
// ---------------------------------------------------------------------------

// Las reglas se escriben contra productIDs (catalog.go) y NO contra UUIDs
// sueltos. Los pesos están calibrados para que dos perfiles opuestos —soltero
// sin hijos vs. casado con tres hijos— produzcan rankings claramente distintos:
// el primero puntúa alto en accidentes/bicicleta/vida-ahorro y el segundo en
// vida/salud/exequial/asistencia familiar. Los productos `empresas-*` no tienen
// reglas a propósito: son B2B y nunca deben salir recomendados a una persona.
var rulesCatalog = []Rule{
	// --- Seguro de vida: protección de quien depende de ti ------------------
	{ID: "r_vida_dep", ProductID: productIDs["seguro-vida"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.6, Active: true,
		Reason: "Tienes dependientes económicos: este seguro les da respaldo ante eventos inesperados."},
	{ID: "r_vida_numdep", ProductID: productIDs["seguro-vida"], Name: "Varios dependientes", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 2.0, Weight: 0.35, Active: true,
		Reason: "Tienes varios dependientes: conviene un respaldo económico familiar más amplio."},
	{ID: "r_vida_pareja", ProductID: productIDs["seguro-vida"], Name: "Con pareja", VariableKey: "marital_status",
		Operator: "in", ExpectedValue: []interface{}{"casado", "union_libre"}, Weight: 0.25, Active: true,
		Reason: "Compartes tu economía con tu pareja: el seguro de vida sostiene ese proyecto si tú faltas."},
	{ID: "r_vida_edad", ProductID: productIDs["seguro-vida"], Name: "Edad de responsabilidad", VariableKey: "age",
		Operator: "gte", ExpectedValue: 30.0, Weight: 0.1, Active: true,
		Reason: "Por tu etapa de vida, un seguro de vida protege a los tuyos."},
	{ID: "r_vida_ingreso", ProductID: productIDs["seguro-vida"], Name: "Ingreso califica vida", VariableKey: "monthly_income",
		Operator: "gte", ExpectedValue: 2000000.0, Weight: 0.15, Active: true,
		Reason: "Con tus ingresos, el plan de vida es sostenible sin apretar tu presupuesto."},

	// --- Vida y ahorro: perfil sin cargas familiares que construye patrimonio
	{ID: "r_ahorro_sindep", ProductID: productIDs["seguro-vida-ahorro"], Name: "Sin dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: false, Weight: 0.35, Active: true,
		Reason: "Hoy nadie depende económicamente de ti: puedes usar la protección también para construir ahorro."},
	{ID: "r_ahorro_soltero", ProductID: productIDs["seguro-vida-ahorro"], Name: "Soltero", VariableKey: "marital_status",
		Operator: "equals", ExpectedValue: "soltero", Weight: 0.3, Active: true,
		Reason: "Siendo soltero, el componente de ahorro te rinde más que una suma asegurada alta."},
	{ID: "r_ahorro_joven", ProductID: productIDs["seguro-vida-ahorro"], Name: "Empieza temprano", VariableKey: "age",
		Operator: "lt", ExpectedValue: 40.0, Weight: 0.2, Active: true,
		Reason: "Empezar joven hace que el componente de ahorro acumule más tiempo."},
	{ID: "r_ahorro_capacidad", ProductID: productIDs["seguro-vida-ahorro"], Name: "Ahorro cubre la prima", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 58000.0, Weight: 0.25, Active: true,
		Reason: "Tu capacidad de ahorro alcanza para la prima con componente de ahorro."},

	// --- Accidentes personales --------------------------------------------
	{ID: "r_acc_viaja", ProductID: productIDs["accidentes-personales"], Name: "Se desplaza seguido", VariableKey: "travels_often",
		Operator: "equals", ExpectedValue: true, Weight: 0.5, Active: true,
		Reason: "Te desplazas con frecuencia: protégete frente a accidentes personales."},
	{ID: "r_acc_bici", ProductID: productIDs["accidentes-personales"], Name: "Se mueve en bici", VariableKey: "rides_bike",
		Operator: "equals", ExpectedValue: true, Weight: 0.3, Active: true,
		Reason: "Moverte en bicicleta te expone más en la vía: la cobertura de accidentes aplica directo."},
	{ID: "r_acc_joven", ProductID: productIDs["accidentes-personales"], Name: "Persona activa", VariableKey: "age",
		Operator: "lt", ExpectedValue: 40.0, Weight: 0.25, Active: true,
		Reason: "Por tu ritmo de vida activo, la cobertura de accidentes te da tranquilidad."},
	{ID: "r_acc_ahorro", ProductID: productIDs["accidentes-personales"], Name: "Ahorro cubre accidentes", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 18000.0, Weight: 0.1, Active: true,
		Reason: "La prima de accidentes personales entra en tu capacidad de ahorro."},

	// --- Renta por hospitalización ----------------------------------------
	{ID: "r_renta_dep", ProductID: productIDs["renta-hospitalizacion"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.3, Active: true,
		Reason: "Si te hospitalizan, la renta diaria cubre lo que tu familia deja de recibir."},
	{ID: "r_renta_ingreso", ProductID: productIDs["renta-hospitalizacion"], Name: "Ingreso ajustado", VariableKey: "monthly_income",
		Operator: "lt", ExpectedValue: 3000000.0, Weight: 0.25, Active: true,
		Reason: "Con tu nivel de ingreso, una incapacidad larga pesa: la renta diaria amortigua ese golpe."},
	{ID: "r_renta_edad", ProductID: productIDs["renta-hospitalizacion"], Name: "Edad de mayor riesgo", VariableKey: "age",
		Operator: "gte", ExpectedValue: 35.0, Weight: 0.2, Active: true,
		Reason: "A partir de esta edad crece la probabilidad de hospitalización."},

	// --- Diagnóstico de cáncer ---------------------------------------------
	{ID: "r_cancer_edad", ProductID: productIDs["diagnostico-cancer"], Name: "Edad de tamizaje", VariableKey: "age",
		Operator: "gte", ExpectedValue: 40.0, Weight: 0.45, Active: true,
		Reason: "Desde los 40 aumenta la incidencia: este plan paga al diagnóstico, sin esperar tratamiento."},
	{ID: "r_cancer_dep", ProductID: productIDs["diagnostico-cancer"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.2, Active: true,
		Reason: "Un diagnóstico así golpea la economía de toda la familia."},

	// --- Póliza de salud ----------------------------------------------------
	{ID: "r_salud_familia", ProductID: productIDs["poliza-salud"], Name: "Familia grande", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 3.0, Weight: 0.4, Active: true,
		Reason: "Con tres o más personas a cargo, una póliza de salud familiar sale mejor que atenciones sueltas."},
	{ID: "r_salud_ingreso", ProductID: productIDs["poliza-salud"], Name: "Ingreso suficiente", VariableKey: "monthly_income",
		Operator: "gte", ExpectedValue: 5000000.0, Weight: 0.35, Active: true,
		Reason: "Tus ingresos sostienen una póliza de salud completa."},
	{ID: "r_salud_pareja", ProductID: productIDs["poliza-salud"], Name: "Con pareja", VariableKey: "marital_status",
		Operator: "in", ExpectedValue: []interface{}{"casado", "union_libre"}, Weight: 0.15, Active: true,
		Reason: "La póliza cubre también a tu pareja bajo el mismo plan."},

	// --- Exequial familiar --------------------------------------------------
	{ID: "r_exeq_numdep", ProductID: productIDs["exequial-familiar"], Name: "Varios dependientes", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 2.0, Weight: 0.45, Active: true,
		Reason: "El plan exequial ampara a todo el grupo familiar con una sola cuota."},
	{ID: "r_exeq_edad", ProductID: productIDs["exequial-familiar"], Name: "Edad del titular", VariableKey: "age",
		Operator: "gte", ExpectedValue: 45.0, Weight: 0.3, Active: true,
		Reason: "A tu edad conviene tener el servicio exequial resuelto y sin trámites para los tuyos."},
	{ID: "r_exeq_pareja", ProductID: productIDs["exequial-familiar"], Name: "Con pareja", VariableKey: "marital_status",
		Operator: "in", ExpectedValue: []interface{}{"casado", "union_libre"}, Weight: 0.2, Active: true,
		Reason: "Tu pareja queda incluida en el mismo plan familiar."},

	// --- Mascotas -----------------------------------------------------------
	{ID: "r_masc_has", ProductID: productIDs["seguro-mascotas"], Name: "Tiene mascota", VariableKey: "has_pet",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Tienes mascota en casa: cubre el cuidado de tu perro o gato."},
	{ID: "r_masc_tipo", ProductID: productIDs["seguro-mascotas"], Name: "Perro o gato", VariableKey: "pet_type",
		Operator: "in", ExpectedValue: []interface{}{"perro", "gato"}, Weight: 0.2, Active: true,
		Reason: "Tu mascota entra en la cobertura del plan."},
	{ID: "r_masc_ahorro", ProductID: productIDs["seguro-mascotas"], Name: "Ahorro cubre mascota", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 22000.0, Weight: 0.1, Active: true,
		Reason: "La prima del plan de mascotas cabe en lo que puedes destinar al mes."},

	// --- Autos y motos ------------------------------------------------------
	{ID: "r_auto_has", ProductID: productIDs["autos-motos"], Name: "Tiene vehículo", VariableKey: "has_vehicle",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Tienes vehículo: el todo riesgo cubre daños, hurto y responsabilidad civil."},
	{ID: "r_auto_tipo", ProductID: productIDs["autos-motos"], Name: "Carro o moto", VariableKey: "vehicle_type",
		Operator: "in", ExpectedValue: []interface{}{"carro", "moto"}, Weight: 0.2, Active: true,
		Reason: "Tu vehículo entra en las modalidades del plan."},
	{ID: "r_auto_viaja", ProductID: productIDs["autos-motos"], Name: "Rueda mucho", VariableKey: "travels_often",
		Operator: "equals", ExpectedValue: true, Weight: 0.15, Active: true,
		Reason: "Te desplazas seguido: más kilómetros, más exposición en la vía."},

	// --- Hogar --------------------------------------------------------------
	{ID: "r_hogar_propia", ProductID: productIDs["hogar"], Name: "Vivienda propia", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "propia", Weight: 0.6, Active: true,
		Reason: "Vives en casa propia: protege tu vivienda y los bienes contenidos en ella."},
	{ID: "r_hogar_arr", ProductID: productIDs["hogar"], Name: "Vivienda arrendada", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "arrendada", Weight: 0.3, Active: true,
		Reason: "Aunque arriendas, puedes proteger tus bienes dentro del hogar."},
	{ID: "r_hogar_familia", ProductID: productIDs["hogar"], Name: "Hogar con familia", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 2.0, Weight: 0.15, Active: true,
		Reason: "En un hogar con varias personas hay más bienes y más uso: sube el riesgo de daño."},
	{ID: "r_hogar_ahorro", ProductID: productIDs["hogar"], Name: "Ahorro cubre hogar", VariableKey: "saving_capacity",
		Operator: "gte", ExpectedValue: 32000.0, Weight: 0.1, Active: true,
		Reason: "Tu capacidad de ahorro cubre la prima mensual del plan hogar."},

	// --- Bicicletas ---------------------------------------------------------
	{ID: "r_bici_usa", ProductID: productIDs["bicicletas"], Name: "Se mueve en bici", VariableKey: "rides_bike",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Te mueves en bicicleta: cubre hurto, daños y responsabilidad civil."},
	{ID: "r_bici_edad", ProductID: productIDs["bicicletas"], Name: "Ciclista urbano", VariableKey: "age",
		Operator: "lt", ExpectedValue: 45.0, Weight: 0.15, Active: true,
		Reason: "Es el perfil que más usa la bici como medio de transporte diario."},

	// --- Arrendamiento ------------------------------------------------------
	{ID: "r_arr_arrienda", ProductID: productIDs["arrendamiento"], Name: "Vive en arriendo", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "arrendada", Weight: 0.6, Active: true,
		Reason: "Vives en arriendo: el amparo respalda el canon y evita codeudores."},
	{ID: "r_arr_ingreso", ProductID: productIDs["arrendamiento"], Name: "Ingreso verificable", VariableKey: "monthly_income",
		Operator: "gte", ExpectedValue: 1500000.0, Weight: 0.15, Active: true,
		Reason: "Con tu ingreso calificas al amparo de arrendamiento."},

	// --- Asistencia médica familiar ----------------------------------------
	{ID: "r_asmed_familia", ProductID: productIDs["asistencia-medica-familiar"], Name: "Familia grande", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 3.0, Weight: 0.5, Active: true,
		Reason: "Con varios a cargo, la asistencia médica familiar resuelve consultas sin pagar cada una."},
	{ID: "r_asmed_dep", ProductID: productIDs["asistencia-medica-familiar"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.3, Active: true,
		Reason: "Tus dependientes quedan cubiertos por la misma asistencia."},
	{ID: "r_asmed_presupuesto", ProductID: productIDs["asistencia-medica-familiar"], Name: "Presupuesto ajustado", VariableKey: "saving_capacity",
		Operator: "lt", ExpectedValue: 60000.0, Weight: 0.2, Active: true,
		Reason: "Es la alternativa asequible cuando una póliza de salud completa no cabe en el presupuesto."},

	// --- Asistencia mascotas ------------------------------------------------
	{ID: "r_asmasc_has", ProductID: productIDs["asistencia-mascotas"], Name: "Tiene mascota", VariableKey: "has_pet",
		Operator: "equals", ExpectedValue: true, Weight: 0.4, Active: true,
		Reason: "Tienes mascota: la asistencia cubre urgencias y orientación veterinaria."},
	{ID: "r_asmasc_presupuesto", ProductID: productIDs["asistencia-mascotas"], Name: "Presupuesto ajustado", VariableKey: "saving_capacity",
		Operator: "lt", ExpectedValue: 25000.0, Weight: 0.25, Active: true,
		Reason: "Cuesta menos que el seguro de mascotas y cubre lo esencial."},

	// --- Asistencia jurídica ------------------------------------------------
	{ID: "r_jur_arriendo", ProductID: productIDs["asistencia-juridica"], Name: "Vive en arriendo", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "arrendada", Weight: 0.25, Active: true,
		Reason: "Los contratos de arriendo son la consulta jurídica más frecuente."},
	{ID: "r_jur_vehiculo", ProductID: productIDs["asistencia-juridica"], Name: "Tiene vehículo", VariableKey: "has_vehicle",
		Operator: "equals", ExpectedValue: true, Weight: 0.2, Active: true,
		Reason: "Con vehículo puedes necesitar acompañamiento jurídico tras un siniestro."},
	{ID: "r_jur_credito", ProductID: productIDs["asistencia-juridica"], Name: "Con crédito", VariableKey: "has_credit",
		Operator: "equals", ExpectedValue: true, Weight: 0.2, Active: true,
		Reason: "Tener crédito vigente hace útil la asesoría jurídica sobre obligaciones."},

	// --- Vida deudor --------------------------------------------------------
	{ID: "r_deudor_credito", ProductID: productIDs["seguro-vida-deudor"], Name: "Con crédito vigente", VariableKey: "has_credit",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Tienes crédito vigente: este seguro evita que la deuda pase a tu familia."},
	{ID: "r_deudor_dep", ProductID: productIDs["seguro-vida-deudor"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.2, Active: true,
		Reason: "Tus dependientes no heredarían el saldo del crédito."},

	// --- Desempleo ----------------------------------------------------------
	{ID: "r_desem_credito", ProductID: productIDs["seguro-desempleo"], Name: "Con crédito vigente", VariableKey: "has_credit",
		Operator: "equals", ExpectedValue: true, Weight: 0.5, Active: true,
		Reason: "Con crédito vigente, quedarte sin empleo compromete las cuotas: este amparo las cubre un tiempo."},
	{ID: "r_desem_dep", ProductID: productIDs["seguro-desempleo"], Name: "Con dependientes", VariableKey: "has_dependents",
		Operator: "equals", ExpectedValue: true, Weight: 0.3, Active: true,
		Reason: "Tu ingreso sostiene a otras personas: perderlo tiene efecto inmediato en casa."},
	{ID: "r_desem_numdep", ProductID: productIDs["seguro-desempleo"], Name: "Familia grande", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 3.0, Weight: 0.2, Active: true,
		Reason: "Con tres o más personas a cargo, el colchón ante desempleo pesa más."},

	// --- Incendio -----------------------------------------------------------
	{ID: "r_inc_propia", ProductID: productIDs["seguro-incendio"], Name: "Vivienda propia", VariableKey: "housing_type",
		Operator: "equals", ExpectedValue: "propia", Weight: 0.5, Active: true,
		Reason: "La vivienda propia es tu patrimonio principal: el amparo de incendio la reconstruye."},
	{ID: "r_inc_familia", ProductID: productIDs["seguro-incendio"], Name: "Hogar con familia", VariableKey: "num_dependents",
		Operator: "gte", ExpectedValue: 2.0, Weight: 0.1, Active: true,
		Reason: "Un siniestro así dejaría sin techo a varias personas."},

	// --- Asistencia médica en viajes ---------------------------------------
	{ID: "r_viaje_exterior", ProductID: productIDs["asistencia-medica-viajes"], Name: "Viaja al exterior", VariableKey: "travels_abroad",
		Operator: "equals", ExpectedValue: true, Weight: 0.7, Active: true,
		Reason: "Sales del país: fuera de Colombia tu sistema de salud no te cubre."},
	{ID: "r_viaje_frecuente", ProductID: productIDs["asistencia-medica-viajes"], Name: "Viaja seguido", VariableKey: "travels_often",
		Operator: "equals", ExpectedValue: true, Weight: 0.25, Active: true,
		Reason: "Viajas con frecuencia: la asistencia aplica en cada desplazamiento."},
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
			Coverages: productCoverages(p),
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

	// Cotización y vinculación: extensión del mock sobre el contrato real
	// (ver enrollment.go), necesaria para cerrar la venta dentro del chat.
	quotes      map[string]*Quote
	enrollments map[string]*Enrollment
}

func newStore() *store {
	return &store{
		users:       map[string]*User{},
		convs:       map[string]*Conversation{},
		vars:        map[string]map[string]UserVariable{},
		quotes:      map[string]*Quote{},
		enrollments: map[string]*Enrollment{},
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
	s.mu.Lock()
	for _, rl := range rulesCatalog {
		if pid == "" || rl.ProductID == pid {
			out = append(out, rl)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// POST /api/v1/rules — crea una regla (RuleCreate del spec real). Idempotente
// por Name: re-seed no duplica.
func (s *store) handleRuleCreate(w http.ResponseWriter, r *http.Request) {
	var in Rule
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ProductID == "" || in.Name == "" || in.VariableKey == "" {
		writeErr(w, http.StatusUnprocessableEntity, "product_id, name and variable_key required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, rl := range rulesCatalog {
		if rl.Name == in.Name { // upsert por nombre
			in.ID = rl.ID
			rulesCatalog[i] = in
			writeJSON(w, http.StatusCreated, in)
			return
		}
	}
	in.ID = uuid4()
	rulesCatalog = append(rulesCatalog, in)
	writeJSON(w, http.StatusCreated, in)
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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok", "service": "mock-protege", "products": len(products),
		// Declarado explícitamente: estas rutas NO existen en el OpenAPI real.
		"extensions": []string{"POST /api/v1/quotes", "POST /api/v1/enrollments"},
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

// routes arma el router. Está separado de main() para que los tests puedan
// ejercitar la API completa por HTTP, sin abrir un puerto.
func routes(s *store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/products", s.handleProducts)
	mux.HandleFunc("GET /api/v1/questions", s.handleQuestions)
	mux.HandleFunc("GET /api/v1/rules", s.handleRules)
	mux.HandleFunc("POST /api/v1/rules", s.handleRuleCreate)
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
	// Extensión del mock (no existe en el OpenAPI real): cierre de la venta.
	mux.HandleFunc("POST /api/v1/quotes", s.handleQuoteCreate)
	mux.HandleFunc("POST /api/v1/enrollments", s.handleEnrollmentCreate)
	mux.HandleFunc("GET /api/v1/enrollments/{id}", s.handleEnrollmentGet)
	mux.HandleFunc("GET /api/v1/users/{user_id}/enrollments", s.handleUserEnrollments)
	return mux
}

func main() {
	addr := ":9000"
	log.Printf("mock-protege (Colsubsidio Protege API · demo) on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(routes(newStore()))))
}
