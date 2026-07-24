package main

import (
	"sync"
	"testing"
	"time"
)

// ---------- WhatsAppSessions ----------

func TestSessionsResolveRegister(t *testing.T) {
	s := NewWhatsAppSessions()
	// first time: new
	if _, isNew := s.Resolve("+57300"); !isNew {
		t.Fatal("first Resolve should be new")
	}
	s.Register("+57300", "conv-1")
	// within window: reuse same conv
	got, isNew := s.Resolve("+57300")
	if isNew || got != "conv-1" {
		t.Errorf("reuse = (%q,%v), want (conv-1,false)", got, isNew)
	}
	if p := s.PhoneFor("conv-1"); p != "+57300" {
		t.Errorf("PhoneFor = %q, want +57300", p)
	}
}

func TestSessionsWindowExpiry(t *testing.T) {
	s := NewWhatsAppSessions()
	fake := time.Now()
	s.now = func() time.Time { return fake }
	s.Register("+57300", "conv-old")
	// advance beyond the 24h window
	fake = fake.Add(waWindow + time.Minute)
	if _, isNew := s.Resolve("+57300"); !isNew {
		t.Error("session past the 24h window should be treated as new")
	}
}

func TestSessionsClose(t *testing.T) {
	s := NewWhatsAppSessions()
	s.Register("+57300", "conv-1")
	s.Close("conv-1")
	if p := s.PhoneFor("conv-1"); p != "" {
		t.Errorf("PhoneFor after Close = %q, want empty", p)
	}
	if _, isNew := s.Resolve("+57300"); !isNew {
		t.Error("Resolve after Close should be new")
	}
}

func TestSessionsCloseKeepsNewerSession(t *testing.T) {
	// Closing an old convID must not wipe a newer session on the same phone.
	s := NewWhatsAppSessions()
	s.Register("+57300", "conv-old")
	s.Register("+57300", "conv-new") // reassign phone to a newer conversation
	s.Close("conv-old")              // stale close
	if got := s.PhoneFor("conv-new"); got != "+57300" {
		t.Errorf("newer session lost after stale Close: PhoneFor=%q", got)
	}
}

func TestSessionsList(t *testing.T) {
	s := NewWhatsAppSessions()
	if got := s.List(); len(got) != 0 {
		t.Fatalf("empty List = %d", len(got))
	}
	s.Register("+57300", "conv-1")
	s.Register("+57311", "conv-2")
	got := s.List()
	if len(got) != 2 {
		t.Fatalf("List = %d, want 2", len(got))
	}
	seen := map[string]string{}
	for _, si := range got {
		seen[si.Phone] = si.ConvID
		if si.LastActivity.IsZero() {
			t.Error("LastActivity should be set")
		}
	}
	if seen["+57300"] != "conv-1" || seen["+57311"] != "conv-2" {
		t.Errorf("List mapping wrong: %v", seen)
	}
	// expirada no aparece
	s2 := NewWhatsAppSessions()
	fake := time.Now()
	s2.now = func() time.Time { return fake }
	s2.Register("+57300", "conv-old")
	fake = fake.Add(waWindow + time.Minute)
	if got := s2.List(); len(got) != 0 {
		t.Errorf("expired session listed: %v", got)
	}
	// cerrada no aparece
	s.Close("conv-1")
	for _, si := range s.List() {
		if si.ConvID == "conv-1" {
			t.Error("closed session still listed")
		}
	}
}

func TestSessionsConcurrent(t *testing.T) {
	s := NewWhatsAppSessions()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			phone := "+5730" + string(rune('0'+n%10))
			s.Resolve(phone)
			s.Register(phone, "c"+string(rune('a'+n%26)))
			s.PhoneFor("c" + string(rune('a'+n%26)))
		}(i)
	}
	wg.Wait() // -race must stay clean
}

// ---------- replyEvent (channel -> outbound event routing, pure) ----------

func TestReplyEventWhatsApp(t *testing.T) {
	typ, producer, payload := replyEvent("whatsapp", "Hola cliente")
	if typ != MESSAGE_SENT {
		t.Errorf("type = %q, want MESSAGE_SENT", typ)
	}
	if producer != "whatsapp_adapter" {
		t.Errorf("producer = %q, want whatsapp_adapter", producer)
	}
	if payload["text"] != "Hola cliente" {
		t.Errorf("text = %v", payload["text"])
	}
	if payload["channel"] != "whatsapp" {
		t.Errorf("channel = %v, want whatsapp", payload["channel"])
	}
}

func TestReplyEventVoiceDefault(t *testing.T) {
	for _, ch := range []string{"", "web", "voice", "webrtc"} {
		typ, producer, _ := replyEvent(ch, "hola")
		if typ != VOICE_SENT {
			t.Errorf("channel %q: type = %q, want VOICE_SENT", ch, typ)
		}
		if producer != "voice_adapter" {
			t.Errorf("channel %q: producer = %q, want voice_adapter", ch, producer)
		}
	}
}
