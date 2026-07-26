package api

import (
	"log"
	"math/rand"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const ringSize = 500

// EventStream wraps the backend's /ws feed with auto-reconnect and a small
// ring buffer so a module that mounts mid-call still sees recent history.
type EventStream struct {
	wsURL string
	out   chan Event
	state atomic.Int32

	ring    []Event
	ringPos int
	ringLen int

	stop chan struct{}
}

func NewEventStream(httpBaseURL string) *EventStream {
	wsURL := toWSURL(httpBaseURL, "/ws")
	s := &EventStream{
		wsURL: wsURL,
		out:   make(chan Event, 256),
		ring:  make([]Event, ringSize),
		stop:  make(chan struct{}),
	}
	s.state.Store(int32(StreamConnecting))
	return s
}

func toWSURL(httpBaseURL, path string) string {
	u, err := url.Parse(httpBaseURL)
	if err != nil {
		return httpBaseURL + path
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = path
	return u.String()
}

// Start begins the connect/read/reconnect loop in a background goroutine.
func (s *EventStream) Start() {
	go s.run()
}

func (s *EventStream) Stop() {
	close(s.stop)
}

func (s *EventStream) Out() <-chan Event { return s.out }

func (s *EventStream) State() StreamState { return StreamState(s.state.Load()) }

// Recent returns up to n most-recently-seen events, oldest first.
func (s *EventStream) Recent(n int) []Event {
	if n > s.ringLen {
		n = s.ringLen
	}
	out := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		idx := (s.ringPos - n + i + ringSize) % ringSize
		out = append(out, s.ring[idx])
	}
	return out
}

func (s *EventStream) remember(e Event) {
	s.ring[s.ringPos] = e
	s.ringPos = (s.ringPos + 1) % ringSize
	if s.ringLen < ringSize {
		s.ringLen++
	}
}

func (s *EventStream) run() {
	backoff := 250 * time.Millisecond
	const maxBackoff = 8 * time.Second

	for {
		select {
		case <-s.stop:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
		if err != nil {
			s.state.Store(int32(StreamReconnecting))
			jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
			time.Sleep(backoff + jitter)
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		s.state.Store(int32(StreamLive))
		backoff = 250 * time.Millisecond
		s.readLoop(conn)

		select {
		case <-s.stop:
			return
		default:
		}
		s.state.Store(int32(StreamReconnecting))
	}
}

func (s *EventStream) readLoop(conn *websocket.Conn) {
	defer conn.Close()
	for {
		var ev Event
		if err := conn.ReadJSON(&ev); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) &&
				!strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("ws read error: %v", err)
			}
			return
		}
		s.remember(ev)
		select {
		case s.out <- ev:
		default:
			// Drop oldest-consumer-side if nobody is draining fast enough;
			// the ring buffer still has it for Recent().
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
