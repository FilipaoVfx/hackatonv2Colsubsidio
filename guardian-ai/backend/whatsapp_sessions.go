package main

import (
	"sync"
	"time"
)

// waWindow is the WhatsApp 24h service window. A conversation older than this is
// considered closed; the next inbound message opens a fresh one.
const waWindow = 24 * time.Hour

type waSession struct {
	convID       string
	lastActivity time.Time
}

// WhatsAppSessions maps a customer phone to a live conversation (call_id reused
// as conversation_id). WhatsApp is asynchronous and long-lived with no "call"
// boundary, so a session is opened when a customer writes after being inactive
// and closed by intent (end) or window expiry.
type WhatsAppSessions struct {
	mu      sync.Mutex
	byPhone map[string]waSession // phone -> session
	byConv  map[string]string    // convID -> phone
	now     func() time.Time     // injectable clock for tests
}

func NewWhatsAppSessions() *WhatsAppSessions {
	return &WhatsAppSessions{
		byPhone: make(map[string]waSession),
		byConv:  make(map[string]string),
		now:     time.Now,
	}
}

// Resolve returns the live conversation id for a phone. If none exists or the
// previous one is outside the 24h window, it returns isNew=true and the caller
// must open a conversation (engine.Start) then Register the mapping.
func (s *WhatsAppSessions) Resolve(phone string) (convID string, isNew bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.byPhone[phone]; ok && s.now().Sub(sess.lastActivity) < waWindow {
		sess.lastActivity = s.now()
		s.byPhone[phone] = sess
		return sess.convID, false
	}
	return "", true
}

// Register binds a phone to a freshly started conversation (bidirectional).
func (s *WhatsAppSessions) Register(phone, convID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// drop any stale reverse mapping for this phone's old conversation
	if old, ok := s.byPhone[phone]; ok {
		delete(s.byConv, old.convID)
	}
	s.byPhone[phone] = waSession{convID: convID, lastActivity: s.now()}
	s.byConv[convID] = phone
}

// PhoneFor returns the phone bound to a conversation, or "" if unknown. Used by
// the MESSAGE_SENT delivery consumer to know where to send the reply.
func (s *WhatsAppSessions) PhoneFor(convID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byConv[convID]
}

// Close removes the mapping for a finished conversation (on end / hangup).
func (s *WhatsAppSessions) Close(convID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if phone, ok := s.byConv[convID]; ok {
		delete(s.byConv, convID)
		if sess, ok := s.byPhone[phone]; ok && sess.convID == convID {
			delete(s.byPhone, phone)
		}
	}
}

// SessionInfo is a read-only snapshot of a live session (for the debug/API layer).
type SessionInfo struct {
	Phone        string    `json:"phone"`
	ConvID       string    `json:"conversation_id"`
	LastActivity time.Time `json:"last_activity"`
}

// List returns a snapshot of all live sessions (within the 24h window). The UI
// uses it to re-attach to an ongoing conversation after a page reload.
func (s *WhatsAppSessions) List() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SessionInfo{}
	for phone, sess := range s.byPhone {
		if s.now().Sub(sess.lastActivity) < waWindow {
			out = append(out, SessionInfo{Phone: phone, ConvID: sess.convID, LastActivity: sess.lastActivity})
		}
	}
	return out
}
