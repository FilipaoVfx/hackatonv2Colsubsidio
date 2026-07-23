package main

import (
	"log"
	"os"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	bus := NewEventBus()
	store := NewEventStore()
	hub := NewHub()

	// persistence + dashboard consume every event (invariant §7.1)
	bus.Subscribe("*", store.Append)
	bus.Subscribe("*", hub.Broadcast)
	bus.Subscribe("*", func(ev Event) {
		log.Printf("[%s seq=%d] %s <- %s", ev.CallID[:8], ev.Sequence, ev.Type, ev.Producer)
	})

	step := 500 * time.Millisecond
	if v := os.Getenv("STEP_MS"); v != "" {
		if d, err := time.ParseDuration(v + "ms"); err == nil {
			step = d
		}
	}
	orch := NewOrchestrator(bus, step)

	app := fiber.New(fiber.Config{AppName: "Guardian AI"})
	app.Use(cors.New())

	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "guardian-ai", "time": time.Now().UTC()})
	})

	// Trigger a simulated end-to-end call (MVP demo entrypoint).
	app.Post("/api/calls/simulate", func(c *fiber.Ctx) error {
		from := c.Query("from", "+57 300 000 0000")
		id := orch.Run(from)
		return c.JSON(fiber.Map{"call_id": id, "status": "started"})
	})

	// Replay the persisted event log for a call (RN-003).
	app.Get("/api/calls/:id/events", func(c *fiber.Ctx) error {
		return c.JSON(store.Get(c.Params("id")))
	})

	app.Get("/api/calls", func(c *fiber.Ctx) error {
		return c.JSON(store.Calls())
	})

	// Dashboard WebSocket (read-only stream).
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		hub.Add(c)
		defer hub.Remove(c)
		// keep the connection open; ignore inbound frames (RN-004)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))

	addr := ":3000"
	log.Printf("Guardian AI backend on %s (step=%s)", addr, step)
	log.Fatal(app.Listen(addr))
}
