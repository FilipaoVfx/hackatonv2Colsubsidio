package main

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Analytics serves the post-call pipeline view (Pipeline de Llamadas):
// header, phases, scores, insights, transcript and customer profile.
// Read-only over the analytics tables; never touches the live event path.
type Analytics struct{ pool *pgxpool.Pool }

func NewAnalytics(p *pgxpool.Pool) *Analytics { return &Analytics{pool: p} }

type CallListItem struct {
	CallID     string `json:"call_id"`
	CallCode   string `json:"call_code"`
	Customer   string `json:"customer"`
	Phone      string `json:"phone"`
	StartedAt  string `json:"started_at"`
	DurationS  int    `json:"duration_sec"`
	Channel    string `json:"channel"`
	Outcome    string `json:"outcome"`
	Score      int    `json:"score_overall"`
	ScoreLabel string `json:"score_label"`
	Advisor    string `json:"advisor_name"`

	LeadState string  `json:"lead_state"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
}

func (a *Analytics) List(ctx context.Context) ([]CallListItem, error) {
	rows, err := a.pool.Query(ctx, `
		select ca.call_id::text, coalesce(ca.call_code,''), c.full_name, coalesce(c.phone,''),
		       to_char(ca.started_at at time zone 'America/Bogota','YYYY-MM-DD"T"HH24:MI:SS'), coalesce(ca.duration_sec,0),
		       coalesce(ca.channel,''), coalesce(ca.outcome,''), coalesce(ca.score_overall,0),
		       coalesce(ca.score_label,''), coalesce(ca.advisor_name,''),
		       coalesce(ca.lead_state,''), coalesce(ca.tokens_in,0), coalesce(ca.tokens_out,0),
		       coalesce(ca.cost_usd,0)
		from public.call_analytics ca
		join public.customers c using (customer_id)
		order by ca.started_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CallListItem{}
	for rows.Next() {
		var i CallListItem
		if err := rows.Scan(&i.CallID, &i.CallCode, &i.Customer, &i.Phone, &i.StartedAt,
			&i.DurationS, &i.Channel, &i.Outcome, &i.Score, &i.ScoreLabel, &i.Advisor,
			&i.LeadState, &i.TokensIn, &i.TokensOut, &i.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// KPIs returns the basic Guardian funnel aggregates (spec §12 subset).
func (a *Analytics) KPIs(ctx context.Context) (map[string]interface{}, error) {
	row := a.pool.QueryRow(ctx, `
		select count(*) filter (where channel='whatsapp'),
		       count(*) filter (where lead_state in ('CLOSING','READY_FOR_ADVISOR','COMPLETED') and channel='whatsapp'),
		       count(*) filter (where lead_state='NURTURING' and channel='whatsapp'),
		       coalesce(sum(tool_calls),0), coalesce(sum(tokens_in),0), coalesce(sum(tokens_out),0),
		       coalesce(sum(cost_usd),0), coalesce(avg(nullif(avg_llm_latency_ms,0)),0),
		       coalesce(sum(variables_captured),0)
		from public.call_analytics`)
	var leads, ready, nurturing, toolCalls, tokIn, tokOut, varsCaptured int64
	var cost, avgLat float64
	if err := row.Scan(&leads, &ready, &nurturing, &toolCalls, &tokIn, &tokOut, &cost, &avgLat, &varsCaptured); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"leads_whatsapp":       leads,
		"leads_ready":          ready,
		"leads_nurturing":      nurturing,
		"tool_calls":           toolCalls,
		"tokens_in":            tokIn,
		"tokens_out":           tokOut,
		"cost_usd":             cost,
		"avg_llm_latency_ms":   avgLat,
		"variables_captured":   varsCaptured,
	}, nil
}

// Detail returns the full record backing the Pipeline view.
func (a *Analytics) Detail(ctx context.Context, callID string) (map[string]interface{}, error) {
	head := map[string]interface{}{}
	var (
		callCode, advisor, advisorVoice, channel, outcome, startedAt, scoreLabel, recording string
		duration, score                                                                     int
		name, phone, marital, occupation, income, city                                      string
		age, children                                                                       int
		isNew                                                                               bool
	)
	err := a.pool.QueryRow(ctx, `
		select coalesce(ca.call_code,''), coalesce(ca.advisor_name,''), coalesce(ca.advisor_voice,''),
		       coalesce(ca.channel,''), coalesce(ca.outcome,''),
		       to_char(ca.started_at at time zone 'America/Bogota','YYYY-MM-DD"T"HH24:MI:SS'), coalesce(ca.duration_sec,0),
		       coalesce(ca.score_overall,0), coalesce(ca.score_label,''), coalesce(ca.recording_url,''),
		       c.full_name, coalesce(c.phone,''), coalesce(c.age,0), coalesce(c.marital_status,''),
		       coalesce(c.children,0), coalesce(c.occupation,''), coalesce(c.income_range,''),
		       coalesce(c.city,''), coalesce(c.is_new,false)
		from public.call_analytics ca
		join public.customers c using (customer_id)
		where ca.call_id = $1`, callID).Scan(
		&callCode, &advisor, &advisorVoice, &channel, &outcome, &startedAt, &duration,
		&score, &scoreLabel, &recording, &name, &phone, &age, &marital, &children,
		&occupation, &income, &city, &isNew)
	if err != nil {
		return nil, err
	}
	head["call_id"] = callID
	head["call_code"] = callCode
	head["advisor_name"] = advisor
	head["advisor_voice"] = advisorVoice
	head["channel"] = channel
	head["outcome"] = outcome
	head["started_at"] = startedAt
	head["duration_sec"] = duration
	head["score_overall"] = score
	head["score_label"] = scoreLabel
	head["recording_url"] = recording
	head["customer"] = map[string]interface{}{
		"full_name": name, "phone": phone, "age": age, "marital_status": marital,
		"children": children, "occupation": occupation, "income_range": income,
		"city": city, "is_new": isNew,
	}

	// Phases
	prows, err := a.pool.Query(ctx, `
		select idx, name, start_sec, end_sec, coalesce(pct_of_total,0), coalesce(objective,''),
		       coalesce(agent_pct,0), coalesce(customer_pct,0), coalesce(emotion,''),
		       coalesce(emotion_conf,0), coalesce(checklist,'[]'::jsonb), coalesce(keywords,'[]'::jsonb),
		       coalesce(findings,''), coalesce(status,'')
		from public.call_phases where call_id=$1 order by idx`, callID)
	if err != nil {
		return nil, err
	}
	phases := []map[string]interface{}{}
	for prows.Next() {
		var (
			idx, s, e, pct, ap, cp int
			nm, obj, emo, fnd, st  string
			conf                   float64
			chk, kws               []byte
		)
		if err := prows.Scan(&idx, &nm, &s, &e, &pct, &obj, &ap, &cp, &emo, &conf, &chk, &kws, &fnd, &st); err != nil {
			prows.Close()
			return nil, err
		}
		var checklist, keywords []interface{}
		json.Unmarshal(chk, &checklist)
		json.Unmarshal(kws, &keywords)
		phases = append(phases, map[string]interface{}{
			"idx": idx, "name": nm, "start_sec": s, "end_sec": e, "pct_of_total": pct,
			"objective": obj, "agent_pct": ap, "customer_pct": cp, "emotion": emo,
			"emotion_conf": conf, "checklist": checklist, "keywords": keywords,
			"findings": fnd, "status": st,
		})
	}
	prows.Close()
	head["phases"] = phases

	// Scores
	srows, err := a.pool.Query(ctx,
		`select dimension, label, value from public.call_scores where call_id=$1 order by label`, callID)
	if err != nil {
		return nil, err
	}
	scores := []map[string]interface{}{}
	for srows.Next() {
		var dim, lbl string
		var val int
		if err := srows.Scan(&dim, &lbl, &val); err != nil {
			srows.Close()
			return nil, err
		}
		scores = append(scores, map[string]interface{}{"dimension": dim, "label": lbl, "value": val})
	}
	srows.Close()
	head["scores"] = scores

	// Insights
	irows, err := a.pool.Query(ctx,
		`select text, priority, coalesce(category,'') from public.call_insights where call_id=$1 order by idx`, callID)
	if err != nil {
		return nil, err
	}
	insights := []map[string]interface{}{}
	for irows.Next() {
		var txt, pri, cat string
		if err := irows.Scan(&txt, &pri, &cat); err != nil {
			irows.Close()
			return nil, err
		}
		insights = append(insights, map[string]interface{}{"text": txt, "priority": pri, "category": cat})
	}
	irows.Close()
	head["insights"] = insights

	// Transcript
	trows, err := a.pool.Query(ctx,
		`select idx, speaker, role, text, at_sec, coalesce(dur_sec,0)
		 from public.call_transcript where call_id=$1 order by idx`, callID)
	if err != nil {
		return nil, err
	}
	transcript := []map[string]interface{}{}
	for trows.Next() {
		var idx, at, dur int
		var sp, role, txt string
		if err := trows.Scan(&idx, &sp, &role, &txt, &at, &dur); err != nil {
			trows.Close()
			return nil, err
		}
		transcript = append(transcript, map[string]interface{}{
			"idx": idx, "speaker": sp, "role": role, "text": txt, "at_sec": at, "dur_sec": dur,
		})
	}
	trows.Close()
	head["transcript"] = transcript

	return head, nil
}
