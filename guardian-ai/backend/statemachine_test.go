package main

import "testing"

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to LeadState
		want     bool
	}{
		{StateNew, StateAffiliation, true},
		{StateAffiliation, StateProfile, true},
		{StateProfile, StateFinancial, true},
		{StateFinancial, StateMatching, true},
		{StateMatching, StateReady, true},
		{StateMatching, StateNurturing, true},
		{StateReady, StateCompleted, true},
		{StateNurturing, StateCompleted, true},
		// ilegales (spec: no transiciones arbitrarias)
		{StateNew, StateMatching, false},
		{StateProfile, StateReady, false},
		{StateAffiliation, StateFinancial, false},
		{StateCompleted, StateNew, false},
		{StateReady, StateProfile, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s,%s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestActionAllowed(t *testing.T) {
	if !ActionAllowed(StateProfile, ActionAsk) {
		t.Error("ask debe permitirse en PROFILE_DISCOVERY")
	}
	// Pedir un asesor humano es legítimo en cualquier etapa viva: el motor lo
	// honra caminando flechas legales, no saltando estados.
	if !ActionAllowed(StateProfile, ActionHandoff) {
		t.Error("handoff debe permitirse en PROFILE_DISCOVERY")
	}
	if !ActionAllowed(StateMatching, ActionHandoff) {
		t.Error("handoff debe permitirse en PROJECT_MATCHING")
	}
	if ActionAllowed(StateMatching, ActionRecommend) {
		t.Error("request_recommendation NO debe permitirse en PROJECT_MATCHING (ya se recomendó)")
	}
	// La degradación siempre cae en una acción legal del propio estado.
	for _, s := range []LeadState{StateAffiliation, StateProfile, StateFinancial, StateMatching, StateNurturing} {
		if got := FallbackAction(s); !ActionAllowed(s, got) {
			t.Errorf("FallbackAction(%s) = %q, ilegal en ese estado", s, got)
		}
	}
	if ActionAllowed(StateCompleted, ActionAsk) {
		t.Error("COMPLETED es terminal: sin acciones")
	}
	if ActionAllowed(StateProfile, "delete_user") {
		t.Error("acción fuera del enum jamás permitida")
	}
}

func qz(id, key, ft string, order int, req bool, conds ...ProtegeCondition) ProtegeQuestion {
	return ProtegeQuestion{ID: id, VariableKey: key, FieldType: ft, OrderIndex: order, Required: req, Conditions: conds}
}

func TestMissingQuestionsAndStageComplete(t *testing.T) {
	questions := []ProtegeQuestion{
		qz("q1", "full_name", "text", 1, true),
		qz("q2", "has_dependents", "boolean", 2, true),
		qz("q3", "num_dependents", "number", 3, true,
			ProtegeCondition{DependsOnVariableKey: "has_dependents", Operator: "equals", ExpectedValue: true}),
		qz("q4", "monthly_income", "currency", 9, true),
		qz("q5", "saving_capacity", "currency", 10, true),
		qz("q6", "optional_note", "text", 11, false), // no required: nunca bloquea
	}

	// Sin nada conocido: PROFILE pide q1,q2 (q3 condicional no aplica aún).
	miss := MissingQuestions(StateProfile, questions, map[string]interface{}{})
	if len(miss) != 2 {
		t.Fatalf("profile missing = %d, want 2 (%v)", len(miss), miss)
	}

	// has_dependents=true abre q3.
	known := map[string]interface{}{"full_name": "Ana", "has_dependents": true}
	miss = MissingQuestions(StateProfile, questions, known)
	if len(miss) != 1 || miss[0].VariableKey != "num_dependents" {
		t.Fatalf("branching roto: %v", miss)
	}
	if StageComplete(StateProfile, questions, known) {
		t.Fatal("profile no está completo con q3 pendiente")
	}

	// has_dependents=false NO abre q3: profile completo.
	known2 := map[string]interface{}{"full_name": "Ana", "has_dependents": false}
	if !StageComplete(StateProfile, questions, known2) {
		t.Fatal("profile debe estar completo (condición de q3 no aplica)")
	}

	// FINANCIAL solo ve monthly_income / saving_capacity.
	miss = MissingQuestions(StateFinancial, questions, known2)
	if len(miss) != 2 {
		t.Fatalf("financial missing = %d, want 2", len(miss))
	}
	known2["monthly_income"] = 3000000.0
	known2["saving_capacity"] = 120000.0
	if !StageComplete(StateFinancial, questions, known2) {
		t.Fatal("financial debe estar completo")
	}
}

func TestCondMetOperators(t *testing.T) {
	known := map[string]interface{}{"age": 35.0, "city": "Bogotá", "has_pet": true}
	cases := []struct {
		c    ProtegeCondition
		want bool
	}{
		{ProtegeCondition{"age", "gte", 30.0}, true},
		{ProtegeCondition{"age", "lt", 30.0}, false},
		{ProtegeCondition{"has_pet", "equals", true}, true},
		{ProtegeCondition{"city", "equals", "bogotá"}, true}, // case-insensitive
		{ProtegeCondition{"city", "in", []interface{}{"Cali", "Bogotá"}}, true},
		{ProtegeCondition{"city", "contains", "bogo"}, true},
		{ProtegeCondition{"missing_var", "exists", nil}, false},
		{ProtegeCondition{"age", "exists", nil}, true},
		{ProtegeCondition{"city", "not_equals", "Cali"}, true},
	}
	for i, c := range cases {
		if got := condMet(c.c, known); got != c.want {
			t.Errorf("case %d %+v = %v, want %v", i, c.c, got, c.want)
		}
	}
}
