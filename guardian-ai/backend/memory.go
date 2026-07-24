package main

import "context"

// Memory Manager (spec retrieval.md §6): strategic memory NEVER lives in the
// LLM context — it is rebuilt from the API on EVERY turn. Conversational
// memory (last messages) is kept by the engine; commercial memory (objections,
// interests) is derived from intents and stored as variables too.

type CustomerMemory struct {
	User      *ProtegeUser
	Variables []UserVariable
}

// Known returns the variables as a lookup map (variable_key -> value), the
// shape the state machine consumes.
func (m CustomerMemory) Known() map[string]interface{} {
	out := make(map[string]interface{}, len(m.Variables))
	for _, v := range m.Variables {
		out[v.Key] = v.Value
	}
	return out
}

// BuildMemory reconstructs the customer's strategic memory from the API.
// Always fresh — if two channels talk to the same user, both see the same
// profile. On API error it returns what it could get (user may be nil).
func BuildMemory(ctx context.Context, api *ColsubsidioClient, user *ProtegeUser) (CustomerMemory, error) {
	m := CustomerMemory{User: user}
	if user == nil || user.ID == "" {
		return m, nil
	}
	vars, err := api.GetVariables(ctx, user.ID)
	if err != nil {
		return m, err
	}
	m.Variables = vars
	return m, nil
}

// maxHistory is the conversational-memory window (last messages) sent to the
// LLM. Strategic facts beyond the window survive via the API variables.
const maxHistory = 12

// trimHistory keeps the last maxHistory messages.
func trimHistory(h []oaMessage) []oaMessage {
	if len(h) <= maxHistory {
		return h
	}
	return h[len(h)-maxHistory:]
}
