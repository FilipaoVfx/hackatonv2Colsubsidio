package main

import "sync"

// EventStore is the persistence consumer (ADR-015 / RN-003). It stores the full
// ordered event log per call so a call can be reconstructed by replay.
// MVP: in-memory. Phase 2 swaps this for the Supabase/pgx repository — same
// Handler interface, no change to producers.
type EventStore struct {
	mu  sync.RWMutex
	log map[string][]Event // call_id -> ordered events
}

func NewEventStore() *EventStore {
	return &EventStore{log: make(map[string][]Event)}
}

// Append is subscribed to "*": every event is persisted (invariant §7.1).
func (s *EventStore) Append(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log[ev.CallID] = append(s.log[ev.CallID], ev)
}

func (s *EventStore) Get(callID string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.log[callID]))
	copy(out, s.log[callID])
	return out
}

func (s *EventStore) Calls() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.log))
	for id := range s.log {
		ids = append(ids, id)
	}
	return ids
}
