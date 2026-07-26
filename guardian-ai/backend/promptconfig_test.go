package main

import (
	"strings"
	"testing"
)

// Fase 2 del Agent Studio: los controles visuales mueven el prompt. Estos tests
// fijan DOS cosas: que cada perilla se traduce a la instrucción esperada, y que
// ninguna configuración —por extrema que sea— puede quitar las reglas duras.

// tunedConfig es una configuración deliberadamente opuesta a la de fábrica:
// seca, corporativa, orientada al cierre y sin adornos.
func tunedConfig() AgentConfig {
	cfg := DefaultConfig()
	cfg.Persona.AgentName = "Asesora Colsubsidio"
	cfg.Persona.Empathy = 2
	cfg.Persona.Formality = 9
	cfg.Persona.Closeness = 2
	cfg.Persona.Persuasion = 9
	cfg.Persona.Proactivity = 10
	cfg.Persona.Length = "breve"
	cfg.Persona.Emojis = false
	cfg.Persona.Humor = false
	cfg.Sales.Goals = []string{"cerrar_venta", "agendar_llamada", "resolver_dudas"}
	cfg.Safety.Forbid = []string{"consejos_legales", "consejos_medicos"}
	cfg.Safety.Level = "bajo"
	return cfg
}

func TestPersonaPhrasesFollowTheSliders(t *testing.T) {
	in := promptFixture()
	in.Config = tunedConfig()
	got := BuildSystemPrompt(in)

	for _, want := range []string{
		`Eres "Asesora Colsubsidio"`,
		"Trata de usted, con registro corporativo",      // formalidad 9
		"Mantén distancia profesional",                  // cercanía 2
		"No te detengas en emociones",                   // empatía 2
		"Propón el cierre de forma activa",              // persuasión 9
		"Lleva tú la conversación",                      // proactividad 10
		"Responde en 1-2 frases.",                       // longitud breve
		"No uses emojis.",                               // emojis off
		"1. cerrar tú mismo la vinculación", // primer objetivo
		"3. resolver las dudas",                         // tercer objetivo
		"Nunca vas a dar asesoría legal.",
		"Nunca vas a dar asesoría médica.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("el prompt no refleja la configuración: falta %q", want)
		}
	}
	for _, unwanted := range []string{
		"Responde en 2-4 frases",             // longitud de fábrica
		"Como máximo un emoji",               // emojis de fábrica
		"Ante cualquier duda, dilo con clar", // nivel alto, aquí es bajo
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("el prompt conserva algo que la configuración cambió: %q", unwanted)
		}
	}
}

func TestGoalOrderIsPriorityOrder(t *testing.T) {
	in := promptFixture()
	cfg := DefaultConfig()
	cfg.Sales.Goals = []string{"derivar_humano", "resolver_dudas"}
	in.Config = cfg
	got := BuildSystemPrompt(in)

	first := strings.Index(got, "1. dejar el lead listo")
	second := strings.Index(got, "2. resolver las dudas")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("los objetivos no salieron en el orden configurado:\n%s", got)
	}
	if !strings.Contains(got, "manda el que esté más arriba") {
		t.Error("falta la regla de desempate entre objetivos")
	}
}

// TestHardRulesSurviveAnyConfig es el test que protege el motor de su propia
// consola: se recorren configuraciones extremas y en TODAS deben seguir
// presentes las reglas que no son negociables. Si alguien añade una perilla que
// permita quitarlas, este test lo delata.
func TestHardRulesSurviveAnyConfig(t *testing.T) {
	hardRules := []string{
		"Las decisiones de elegibilidad, subsidios y recomendación las toma el sistema, NUNCA tú.",
		"NUNCA inventes productos, precios, subsidios, reglas ni beneficios.",
		"Máximo UNA pregunta por mensaje",
		"Catálogo REAL (solo puedes mencionar estos productos):",
		"Responde SOLO el JSON del esquema.",
	}

	var configs []AgentConfig
	for _, extreme := range []int{1, 5, 10} {
		for _, length := range lengthOptions {
			for _, level := range safetyLevels {
				cfg := DefaultConfig()
				cfg.Persona.Empathy = extreme
				cfg.Persona.Formality = extreme
				cfg.Persona.Closeness = extreme
				cfg.Persona.Persuasion = extreme
				cfg.Persona.Proactivity = extreme
				cfg.Persona.Length = length
				cfg.Persona.Emojis = extreme > 5
				cfg.Persona.Humor = extreme > 5
				cfg.Safety.Level = level
				cfg.Safety.Forbid = nil // el administrador desmarca TODO
				cfg.Sales.Goals = []string{"cerrar_venta"}
				configs = append(configs, cfg)
			}
		}
	}

	for _, cfg := range configs {
		if errs := cfg.Validate(); len(errs) > 0 {
			t.Fatalf("configuración de prueba inválida: %v", errs)
		}
		in := promptFixture()
		in.Config = cfg
		got := BuildSystemPrompt(in)
		for _, rule := range hardRules {
			if !strings.Contains(got, rule) {
				t.Fatalf("configuración (empatía=%d, %s, seguridad=%s) hizo desaparecer una regla dura: %q",
					cfg.Persona.Empathy, cfg.Persona.Length, cfg.Safety.Level, rule)
			}
		}
	}
}

// TestPromptFallsBackToDefaultsOnZeroConfig: un llamador que no conozca el
// Studio (tests antiguos, herramientas internas) sigue obteniendo el agente de
// fábrica en vez de un prompt mutilado.
func TestPromptFallsBackToDefaultsOnZeroConfig(t *testing.T) {
	in := promptFixture()
	in.Config = AgentConfig{}
	got := BuildSystemPrompt(in)
	if !strings.Contains(got, `Eres "Guardian"`) || !strings.Contains(got, "Responde en 2-4 frases") {
		t.Errorf("el cero-valor no cayó a los defaults de fábrica:\n%s", got)
	}
}

// TestConfigPromptBudget recorre TODAS las combinaciones de tramo del
// vocabulario cerrado (3 tramos × 5 perillas × longitud × nivel × emojis ×
// humor, con el catálogo completo de objetivos y prohibiciones y el nombre más
// largo posible) y comprueba que ni la peor de ellas se pasa del presupuesto
// declarado en el plan. Es una promesa sobre el coste por turno: mover
// controles no puede inflar el prompt sin que este test lo diga.
func TestConfigPromptBudget(t *testing.T) {
	buckets := []int{1, 5, 10} // bajo, medio, alto: cubren las tres frases
	worst, worstCfg := 0, AgentConfig{}

	base := DefaultConfig()
	base.Persona.AgentName = strings.Repeat("Ñ", maxAgentNameLen) // el más caro en bytes
	base.Sales.Goals = append([]string(nil), SalesGoalCatalog...)
	base.Safety.Forbid = append([]string(nil), SafetyForbidCatalog...)

	for _, empathy := range buckets {
		for _, formality := range buckets {
			for _, closeness := range buckets {
				for _, persuasion := range buckets {
					for _, proactivity := range buckets {
						for _, length := range lengthOptions {
							for _, level := range safetyLevels {
								for _, emojis := range []bool{true, false} {
									for _, humor := range []bool{true, false} {
										cfg := base.Clone()
										cfg.Persona.Empathy = empathy
										cfg.Persona.Formality = formality
										cfg.Persona.Closeness = closeness
										cfg.Persona.Persuasion = persuasion
										cfg.Persona.Proactivity = proactivity
										cfg.Persona.Length = length
										cfg.Persona.Emojis = emojis
										cfg.Persona.Humor = humor
										cfg.Safety.Level = level
										if errs := cfg.Validate(); len(errs) > 0 {
											t.Fatalf("una combinación del catálogo cerrado no valida: %v", errs)
										}
										if n := configPromptBytes(cfg); n > worst {
											worst, worstCfg = n, cfg
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if worst > maxConfigPromptBytes {
		t.Errorf("la peor configuración aporta %d bytes al prompt, presupuesto %d.\n"+
			"Si el catálogo creció a propósito, sube maxConfigPromptBytes en el mismo commit y déjalo dicho en el plan.\nPeor caso: %+v",
			worst, maxConfigPromptBytes, worstCfg)
	}
	t.Logf("configuración de fábrica: %d bytes | peor caso: %d de %d (%.0f%% del presupuesto)",
		configPromptBytes(DefaultConfig()), worst, maxConfigPromptBytes,
		100*float64(worst)/float64(maxConfigPromptBytes))
}

// TestPromptSizeStaysReasonable: el prompt viaja en cada turno; una
// configuración cargada no puede dispararlo. El presupuesto de lo que aporta la
// CONFIGURACIÓN lo vigila TestConfigPromptBudget.
func TestPromptSizeStaysReasonable(t *testing.T) {
	in := promptFixture()
	cfg := DefaultConfig()
	cfg.Sales.Goals = append([]string(nil), SalesGoalCatalog...)
	cfg.Safety.Forbid = append([]string(nil), SafetyForbidCatalog...)
	cfg.Persona.Length = "detallada"
	in.Config = cfg

	if n := len(BuildSystemPrompt(in)); n > 8*1024 {
		t.Errorf("prompt de %d bytes con todo activado, want ≤ 8 KB", n)
	}
}
