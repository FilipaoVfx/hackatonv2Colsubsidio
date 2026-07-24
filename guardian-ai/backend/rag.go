package main

import (
	"context"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RAG (spec retrieval.md §9): retrieval EXCLUSIVELY over documentation (FAQ,
// subsidios, productos, glosario). Never business rules, never decisions,
// never scoring — the retrieved text only feeds the "Contexto documental"
// prompt section, explicitly marked "solo para explicar".
//
// Implementation: local .md corpus chunked by heading; embeddings
// (text-embedding-3-small) computed once at startup and held in memory with
// cosine top-k retrieval. Without an OpenAI key it degrades to keyword-overlap
// scoring — same interface, honest "mode" flag in the emitted event.

// knowledgeDir resolves the corpus location (KNOWLEDGE_DIR, default ./knowledge).
func knowledgeDir() string {
	if d := os.Getenv("KNOWLEDGE_DIR"); d != "" {
		return d
	}
	return "knowledge"
}

type Chunk struct {
	Doc     string // file name, e.g. "faq.md"
	Heading string // section heading
	Text    string

	vec []float32 // nil when running in keyword mode
}

type RAG struct {
	chunks   []Chunk
	embedded bool // true when vectors are loaded
	llm      *LLMClient
}

// NewRAG loads the corpus from dir and, if an LLM client with a key is given,
// embeds every chunk in ONE batch request. Missing dir or embed failure never
// break startup — the engine just runs with keyword retrieval (or none).
func NewRAG(dir string, llm *LLMClient) *RAG {
	r := &RAG{llm: llm}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("rag: knowledge dir %q not available: %v (RAG off)", dir, err)
		return r
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		r.chunks = append(r.chunks, chunkMarkdown(e.Name(), string(raw))...)
	}
	if len(r.chunks) == 0 {
		return r
	}
	if llm != nil && llm.key != "" {
		texts := make([]string, len(r.chunks))
		for i, c := range r.chunks {
			texts[i] = c.Heading + "\n" + c.Text
		}
		vecs, err := llm.Embed(context.Background(), texts)
		if err != nil || len(vecs) != len(r.chunks) {
			log.Printf("rag: embeddings unavailable (%v) — keyword mode", err)
		} else {
			for i := range r.chunks {
				r.chunks[i].vec = vecs[i]
			}
			r.embedded = true
		}
	}
	log.Printf("rag: %d chunks loaded (embedded=%v)", len(r.chunks), r.embedded)
	return r
}

func (r *RAG) Enabled() bool { return r != nil && len(r.chunks) > 0 }

// Mode reports how retrieval runs — surfaced in KNOWLEDGE_RETRIEVED.
func (r *RAG) Mode() string {
	if r.embedded {
		return "embeddings"
	}
	return "keyword"
}

// Retrieve returns the top-k most relevant chunks for the query.
func (r *RAG) Retrieve(ctx context.Context, query string, k int) []Chunk {
	if !r.Enabled() || strings.TrimSpace(query) == "" {
		return nil
	}
	type scored struct {
		c Chunk
		s float64
	}
	var all []scored

	if r.embedded && r.llm != nil {
		qv, err := r.llm.Embed(ctx, []string{query})
		if err == nil && len(qv) == 1 {
			for _, c := range r.chunks {
				all = append(all, scored{c, cosine(qv[0], c.vec)})
			}
		}
	}
	if all == nil { // keyword fallback (also when the query embed failed)
		qw := keywords(query)
		for _, c := range r.chunks {
			all = append(all, scored{c, keywordScore(qw, c)})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].s > all[j].s })
	var out []Chunk
	for i := 0; i < len(all) && i < k; i++ {
		if all[i].s <= 0 {
			break
		}
		out = append(out, all[i].c)
	}
	return out
}

// ---- pure helpers ----

// chunkMarkdown splits a document into one chunk per markdown heading section.
// Text before the first heading becomes a chunk with the file name as heading.
func chunkMarkdown(doc, content string) []Chunk {
	var out []Chunk
	heading := doc
	var buf []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		if text != "" {
			out = append(out, Chunk{Doc: doc, Heading: heading, Text: text})
		}
		buf = nil
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(line, "# "))
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

var ragStopwords = map[string]bool{
	"el": true, "la": true, "los": true, "las": true, "de": true, "del": true,
	"un": true, "una": true, "que": true, "como": true, "para": true, "por": true,
	"con": true, "es": true, "en": true, "y": true, "o": true, "se": true,
	"me": true, "mi": true, "tu": true, "su": true, "al": true, "lo": true,
}

func keywords(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?¿¡\"'()")
		if len(w) >= 3 && !ragStopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// keywordScore counts query keywords present in the chunk (heading hits ×2).
func keywordScore(qw []string, c Chunk) float64 {
	head := strings.ToLower(c.Heading)
	body := strings.ToLower(c.Text)
	var s float64
	for _, w := range qw {
		if strings.Contains(head, w) {
			s += 2
		} else if strings.Contains(body, w) {
			s += 1
		}
	}
	return s
}
