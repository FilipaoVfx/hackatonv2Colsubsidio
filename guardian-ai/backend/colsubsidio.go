package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// ColsubsidioClient talks to the "Colsubsidio Protege API" (OpenAPI 0.1.0), the
// server-side conversation + recommendation engine. Guardian AI's WhatsApp
// channel is an adapter in front of it: the API owns the users, the dynamic
// question flow, the profile variables and the rule-based recommendations. This
// client only wraps the endpoints the WhatsApp flow needs.
//
// Base URL + optional bearer token come from the environment. Disabled (nil-safe)
// when COLSUBSIDIO_API_URL is unset, so the demo still runs on the local engine.
type ColsubsidioClient struct {
	base  string
	token string
	http  *http.Client
}

func NewColsubsidioClient() *ColsubsidioClient {
	return &ColsubsidioClient{
		base:  os.Getenv("COLSUBSIDIO_API_URL"),
		token: os.Getenv("COLSUBSIDIO_API_TOKEN"), // optional; spec declares no auth
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *ColsubsidioClient) Enabled() bool { return c.base != "" }

// ---- schemas (subset of the OpenAPI spec actually used) ----

type ProtegeUser struct {
	ID             string `json:"id"`
	DocumentType   string `json:"document_type,omitempty"`
	DocumentNumber string `json:"document_number,omitempty"`
	NIT            string `json:"nit,omitempty"`
	Phone          string `json:"phone,omitempty"`
	Email          string `json:"email,omitempty"`
	FirstName      string `json:"first_name,omitempty"`
	LastName       string `json:"last_name,omitempty"`
}

// ProtegeQuestion mirrors QuestionPublic (+ order_index/conditions from
// QuestionRead, served by GET /questions): a question in the API's catalog.
type ProtegeQuestion struct {
	ID          string             `json:"id"`
	VariableKey string             `json:"variable_key"`
	Text        string             `json:"text"`
	FieldType   string             `json:"field_type"` // text|number|currency|boolean|radio|select|multi_select|date|email|phone
	Required    bool               `json:"required"`
	OrderIndex  int                `json:"order_index"`
	HelpText    string             `json:"help_text"`
	Placeholder string             `json:"placeholder"`
	Options     []interface{}      `json:"options"`
	Conditions  []ProtegeCondition `json:"conditions"`
}

// ProtegeCondition mirrors QuestionConditionRead: the question only applies
// when the referenced variable satisfies operator vs expected_value.
type ProtegeCondition struct {
	DependsOnVariableKey string      `json:"depends_on_variable_key"`
	Operator             string      `json:"operator"` // ConditionOperator enum
	ExpectedValue        interface{} `json:"expected_value"`
}

// VariableValue mirrors VariableValueInput (PUT /users/{id}/variables): a
// profile fact confirmed during the conversation, persisted immediately.
type VariableValue struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Source     string      `json:"source,omitempty"`
	Confidence *float64    `json:"confidence,omitempty"`
}

// UserVariable mirrors UserVariableRead: the API-side stored profile variable.
type UserVariable struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Source     string      `json:"source"`
	Confidence *float64    `json:"confidence"`
}

// ProtegeRule mirrors RuleRead: one weighted business rule of the recommender.
type ProtegeRule struct {
	ID            string      `json:"id"`
	ProductID     string      `json:"product_id"`
	Name          string      `json:"name"`
	VariableKey   string      `json:"variable_key"`
	Operator      string      `json:"operator"`
	ExpectedValue interface{} `json:"expected_value"`
	Weight        float64     `json:"weight"`
	Reason        string      `json:"reason"`
	Active        bool        `json:"active"`
}

// ProtegeProduct mirrors ProductRead: catalog entry used to ground the LLM.
type ProtegeProduct struct {
	ID          string                 `json:"id"`
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    string                 `json:"category"`
	Active      bool                   `json:"active"`
	BasePrice   float64                `json:"base_price"`
	Metadata    map[string]interface{} `json:"metadata_json"`
}

// ProtegeConversation covers the shared fields of ConversationRead and
// ConversationAnswerResponse: both carry the next question and the readiness flag.
type ProtegeConversation struct {
	ID                        string           `json:"id"`
	UserID                    string           `json:"user_id"`
	Channel                   string           `json:"channel"`
	Status                    string           `json:"status"` // new|collecting_data|ready_for_recommendation|completed|cancelled
	CurrentQuestionID         string           `json:"current_question_id"`
	NextQuestion              *ProtegeQuestion `json:"next_question"`
	CanGenerateRecommendation bool             `json:"can_generate_recommendation"`
}

type ProtegeCompleteResult struct {
	ConversationID  string        `json:"conversation_id"`
	Status          string        `json:"status"`
	SnapshotID      string        `json:"snapshot_id"`
	UserID          string        `json:"user_id"`
	Recommendations []interface{} `json:"recommendations"`
}

// ---- requests ----

func (c *ColsubsidioClient) do(ctx context.Context, method, path string, body, out interface{}) error {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("colsubsidio %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("colsubsidio %s %s -> %d: %s", method, path, resp.StatusCode, string(data[:min(len(data), 400)]))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("colsubsidio decode %s: %w", path, err)
		}
	}
	return nil
}

// SearchUserByPhone returns the first user matching the phone, or nil if none.
func (c *ColsubsidioClient) SearchUserByPhone(ctx context.Context, phone string) (*ProtegeUser, error) {
	var users []ProtegeUser
	q := url.Values{"phone": {phone}}
	if err := c.do(ctx, "GET", "/api/v1/users/search?"+q.Encode(), nil, &users); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

// CreateUser registers a new user (buscar-o-crear: called when search is empty).
func (c *ColsubsidioClient) CreateUser(ctx context.Context, u ProtegeUser) (*ProtegeUser, error) {
	var out ProtegeUser
	if err := c.do(ctx, "POST", "/api/v1/users", u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResolveUser searches by phone and creates the user if absent. Returns the user
// and whether it was newly created.
func (c *ColsubsidioClient) ResolveUser(ctx context.Context, phone string) (*ProtegeUser, bool, error) {
	u, err := c.SearchUserByPhone(ctx, phone)
	if err != nil {
		return nil, false, err
	}
	if u != nil {
		return u, false, nil
	}
	created, err := c.CreateUser(ctx, ProtegeUser{Phone: phone})
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

// StartConversation opens a channel="whatsapp" conversation for a user. The
// response carries the first next_question.
func (c *ColsubsidioClient) StartConversation(ctx context.Context, userID, externalSessionID string) (*ProtegeConversation, error) {
	body := map[string]interface{}{
		"user_id":             userID,
		"channel":             "whatsapp",
		"external_session_id": externalSessionID,
	}
	var out ProtegeConversation
	if err := c.do(ctx, "POST", "/api/v1/conversations", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Answer submits the value for the current question and returns the updated
// conversation (with the next question / readiness flag).
func (c *ColsubsidioClient) Answer(ctx context.Context, conversationID, questionID string, value interface{}, source string) (*ProtegeConversation, error) {
	body := map[string]interface{}{
		"question_id": questionID,
		"value":       value,
		"source":      source,
	}
	var out ProtegeConversation
	if err := c.do(ctx, "POST", "/api/v1/conversations/"+conversationID+"/answers", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Complete closes data collection and generates the recommendations.
func (c *ColsubsidioClient) Complete(ctx context.Context, conversationID string, limit int) (*ProtegeCompleteResult, error) {
	path := "/api/v1/conversations/" + conversationID + "/complete"
	if limit > 0 {
		path += "?limit=" + fmt.Sprint(limit)
	}
	var out ProtegeCompleteResult
	if err := c.do(ctx, "POST", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- Guardian Conversation Engine endpoints (spec retrieval.md) ----

// SaveVariables persists confirmed profile facts IMMEDIATELY (spec: never wait
// for the end of the conversation). Upsert batch; returns the stored state.
func (c *ColsubsidioClient) SaveVariables(ctx context.Context, userID string, vars []VariableValue) ([]UserVariable, error) {
	var out []UserVariable
	if err := c.do(ctx, "PUT", "/api/v1/users/"+userID+"/variables", vars, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetVariables returns the user's stored profile variables. This is the ONLY
// source of strategic memory: the engine rebuilds context from here each turn.
func (c *ColsubsidioClient) GetVariables(ctx context.Context, userID string) ([]UserVariable, error) {
	var out []UserVariable
	if err := c.do(ctx, "GET", "/api/v1/users/"+userID+"/variables", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetQuestions lists the active question catalog. The engine uses it to know
// which variables are still missing per lead state (guidance, not a form).
func (c *ColsubsidioClient) GetQuestions(ctx context.Context) ([]ProtegeQuestion, error) {
	var out []ProtegeQuestion
	if err := c.do(ctx, "GET", "/api/v1/questions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRules lists the recommender's weighted rules (optionally per product).
// Fed to the prompt as Business Rules so the LLM EXPLAINS them — never invents.
func (c *ColsubsidioClient) GetRules(ctx context.Context, productID string) ([]ProtegeRule, error) {
	path := "/api/v1/rules"
	if productID != "" {
		path += "?product_id=" + url.QueryEscape(productID)
	}
	var out []ProtegeRule
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProducts lists the real catalog (grounds the prompt: no invented products).
func (c *ColsubsidioClient) GetProducts(ctx context.Context) ([]ProtegeProduct, error) {
	var out []ProtegeProduct
	if err := c.do(ctx, "GET", "/api/v1/products", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GenerateRecommendations scores the user's CURRENT variables against the rules
// (POST /recommendations/users/{id}) — recommendation from natural profiling,
// without walking the rigid answers flow. Untyped array per the spec.
func (c *ColsubsidioClient) GenerateRecommendations(ctx context.Context, userID string, limit int) ([]interface{}, error) {
	path := "/api/v1/recommendations/users/" + userID
	if limit > 0 {
		path += "?limit=" + fmt.Sprint(limit)
	}
	var out struct {
		Recommendations []interface{} `json:"recommendations"`
	}
	if err := c.do(ctx, "POST", path, nil, &out); err != nil {
		return nil, err
	}
	return out.Recommendations, nil
}
