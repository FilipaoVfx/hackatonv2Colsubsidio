package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSampleCSV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "aff.csv")
	csv := `SERIE,GENERO,RANGO_EDAD,RANGO_SALARIAL,CATEGORIA,SEGMENTO_GRUPO_FAMILIAR,SEGMENTO_POBLACIONAL,PIRAMIDE_NUEVA,EMPRESA_FOCO,CIUDAD_AFILIADO,HOTELES,PISCILAGO,DROGUERIA,AGENCIAS,VIVIENDA
1,F,20 a 35 años,Entre 1 y 1.5 SMLV,SIGMA,CHI,PI,XI,EMP_1,BOGOTA D.C.,NO,NO,SI,NO,NO
2,M,36 a 45 años,Entre 3 y 4 SMLV,PI,RHO,PI,XI,EMP_2,SOACHA,SI,NO,NO,SI,NO
`
	if err := os.WriteFile(p, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAffiliatesLoadAndForPhone(t *testing.T) {
	t.Setenv("AFFILIATES_CSV", writeSampleCSV(t))
	a := NewAffiliates()
	if !a.Enabled() || len(a.rows) != 2 {
		t.Fatalf("load = %d rows", len(a.rows))
	}
	af1, ok := a.ForPhone("+57 300 111 2233")
	if !ok {
		t.Fatal("ForPhone debe resolver")
	}
	af2, _ := a.ForPhone("+573001112233") // mismo teléfono, otro formato
	if af1.Serie != af2.Serie {
		t.Errorf("determinismo roto: %s vs %s", af1.Serie, af2.Serie)
	}
}

func TestAffiliateVariablesCanonical(t *testing.T) {
	af := Affiliate{Serie: "9", Gender: "F", AgeRange: "20 a 35 años",
		SalaryRange: "Entre 1 y 1.5 SMLV", City: "BOGOTA D.C.", Drugstore: true}
	vars := af.Variables()
	got := map[string]interface{}{}
	for _, v := range vars {
		got[v.Key] = v.Value
		if v.Source != "colsubsidio_360" {
			t.Errorf("source = %q", v.Source)
		}
	}
	if got["uses_drugstore"] != true || got["city"] != "BOGOTA D.C." {
		t.Errorf("variables = %v", got)
	}
	// monthly_income estimado = 1.25 × SMLV (para que las reglas de ingreso disparen)
	if got["monthly_income"] != 1.25*smlv {
		t.Errorf("monthly_income = %v, want %v", got["monthly_income"], 1.25*smlv)
	}
}

func TestIncomeEstimate(t *testing.T) {
	if v, ok := incomeEstimate("Entre 4 y 6 SMLV"); !ok || v != 5*smlv {
		t.Errorf("4-6 = %v %v", v, ok)
	}
	if _, ok := incomeEstimate("Sin Informacion"); ok {
		t.Error("rango desconocido no debe estimar")
	}
}

func TestDerivedRulesMapToProducts(t *testing.T) {
	products := []ProtegeProduct{
		{ID: "pv", Category: "vida"}, {ID: "ph", Category: "hogar"}, {ID: "pa", Category: "accidentes"},
	}
	rules := derivedRules(products)
	if len(rules) != 4 {
		t.Fatalf("rules = %d, want 4", len(rules))
	}
	for _, r := range rules {
		if r.ProductID == "" || r.Weight <= 0 || r.Weight > 0.5 || r.Reason == "" {
			t.Errorf("regla inválida: %+v", r)
		}
	}
	// sin producto de la categoría, la regla se omite (no rompe)
	if n := len(derivedRules([]ProtegeProduct{{ID: "pv", Category: "vida"}})); n != 2 {
		t.Errorf("con solo vida deben quedar 2 reglas, hay %d", n)
	}
}
