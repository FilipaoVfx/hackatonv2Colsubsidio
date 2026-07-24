package main

import (
	"context"
	"fmt"
	"time"
)

// Tool Calling (spec retrieval.md §7): the agent may ONLY use registered
// tools; anything else is an error. Every execution emits TOOL_CALLED /
// TOOL_EXECUTED with real latency and error, so the dashboard shows the real
// tool activity. Retries: exactly one, and only for idempotent reads.
//
// The tools are executed by the ENGINE deterministically (ADR in 07_FEATURE
// doc): the LLM proposes next_action from a closed enum; it never names tools
// nor arguments directly.

type ToolResult struct {
	Data      interface{}
	Err       error
	LatencyMS int64
}

type Tools struct {
	api *ColsubsidioClient
	bus *EventBus
}

func NewTools(api *ColsubsidioClient, bus *EventBus) *Tools {
	return &Tools{api: api, bus: bus}
}

// idempotentGets are the only tools allowed a single retry.
var idempotentGets = map[string]bool{
	"search_user": true, "get_questions": true, "get_rules": true,
	"get_products": true, "get_variables": true,
}

// registered is the closed tool registry (spec list + the two read helpers the
// memory manager needs).
var registered = map[string]bool{
	"search_user": true, "create_user": true, "get_questions": true,
	"save_variable": true, "get_rules": true, "get_recommendations": true,
	"create_conversation": true, "update_conversation": true, "complete_conversation": true,
	"get_products": true, "get_variables": true,
}

// Run executes one registered tool and emits the tool events on callID.
func (t *Tools) Run(ctx context.Context, callID, name string, args map[string]interface{}) ToolResult {
	if !registered[name] {
		err := fmt.Errorf("tool %q is not registered", name)
		t.bus.Publish(callID, ERROR_OCCURRED, "tool_engine", map[string]interface{}{
			"source": "tool_engine", "code": "unregistered_tool", "message": err.Error(), "recoverable": true,
		})
		return ToolResult{Err: err}
	}
	t.bus.Publish(callID, TOOL_CALLED, "guardian_engine", map[string]interface{}{
		"tool": name, "args": publicArgs(args),
	})
	start := time.Now()
	data, err := t.call(ctx, name, args)
	if err != nil && idempotentGets[name] {
		data, err = t.call(ctx, name, args) // one safe retry
	}
	res := ToolResult{Data: data, Err: err, LatencyMS: time.Since(start).Milliseconds()}
	payload := map[string]interface{}{"tool": name, "latency_ms": res.LatencyMS, "error": nil}
	if err != nil {
		payload["error"] = err.Error()
	}
	t.bus.Publish(callID, TOOL_EXECUTED, "tool_engine", payload)
	return res
}

// publicArgs strips bulky values from the emitted args (events stay light).
func publicArgs(args map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range args {
		switch v := v.(type) {
		case string, bool, float64, int:
			out[k] = v
		default:
			out[k] = fmt.Sprintf("%T", v)
		}
	}
	return out
}

// call maps a registered name to the API client. Args are typed by the caller
// (the engine), never by the LLM.
func (t *Tools) call(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "search_user":
		return t.api.SearchUserByPhone(ctx, str(args["phone"]))
	case "create_user":
		return t.api.CreateUser(ctx, ProtegeUser{Phone: str(args["phone"])})
	case "get_questions":
		return t.api.GetQuestions(ctx)
	case "get_rules":
		return t.api.GetRules(ctx, str(args["product_id"]))
	case "get_products":
		return t.api.GetProducts(ctx)
	case "get_variables":
		return t.api.GetVariables(ctx, str(args["user_id"]))
	case "save_variable":
		vars, _ := args["variables"].([]VariableValue)
		return t.api.SaveVariables(ctx, str(args["user_id"]), vars)
	case "get_recommendations":
		limit, _ := args["limit"].(int)
		return t.api.GenerateRecommendations(ctx, str(args["user_id"]), limit)
	case "create_conversation":
		return t.api.StartConversation(ctx, str(args["user_id"]), str(args["external_session_id"]))
	case "update_conversation":
		return t.api.Answer(ctx, str(args["conversation_id"]), str(args["question_id"]), args["value"], str(args["source"]))
	case "complete_conversation":
		limit, _ := args["limit"].(int)
		return t.api.Complete(ctx, str(args["conversation_id"]), limit)
	}
	return nil, fmt.Errorf("tool %q has no handler", name)
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
