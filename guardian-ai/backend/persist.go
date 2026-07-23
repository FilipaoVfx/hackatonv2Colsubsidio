package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SupabasePersistence is the real persistence consumer (ADR-004/005, RN-003).
// Access is via pgx only — never the Supabase SDK (ADR-004). Subscribed to "*",
// it writes every event to public.events so a call is reconstructable by replay.
type SupabasePersistence struct {
	pool *pgxpool.Pool
}

func NewSupabasePersistence() (*SupabasePersistence, error) {
	url := os.Getenv("SUPABASE_DB_URL")
	if url == "" {
		return nil, nil // persistence optional; nil means "skip"
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	// Session pooler is fine with the default protocol; keep the pool small.
	cfg.MaxConns = 4
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &SupabasePersistence{pool: pool}, nil
}

// Append is subscribed to "*". Failures are logged, never fatal (the demo keeps
// running even if the DB blips).
func (p *SupabasePersistence) Append(ev Event) {
	payload, _ := json.Marshal(ev.Payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ctx,
		`insert into public.events (event_id, call_id, sequence, type, producer, payload, ts)
		 values ($1,$2,$3,$4,$5,$6,$7)
		 on conflict (event_id) do nothing`,
		ev.EventID, ev.CallID, ev.Sequence, ev.Type, ev.Producer, payload, ev.Timestamp)
	if err != nil {
		log.Printf("persist error [%s]: %v", ev.Type, err)
		return
	}

	// Maintain the calls projection on lifecycle/summary events.
	switch ev.Type {
	case CALL_STARTED:
		from, _ := ev.Payload["from"].(string)
		p.pool.Exec(ctx,
			`insert into public.calls (call_id, from_number) values ($1,$2)
			 on conflict (call_id) do nothing`, ev.CallID, from)
	case SUMMARY_GENERATED:
		summary, _ := ev.Payload["summary"].(string)
		profile, _ := json.Marshal(ev.Payload["profile_snapshot"])
		p.pool.Exec(ctx,
			`update public.calls set summary=$2, profile=$3, ended_at=now() where call_id=$1`,
			ev.CallID, summary, profile)
	}
}
