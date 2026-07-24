package main

import (
	"context"
	"testing"
)

func TestChunkMarkdown(t *testing.T) {
	doc := "intro sin heading\n# Subsidios\ntexto de subsidios\nmás texto\n## Requisitos\nrequisitos aquí\n# Vacía\n\n# FAQ\npregunta y respuesta"
	chunks := chunkMarkdown("subsidios.md", doc)
	if len(chunks) != 4 {
		t.Fatalf("chunks = %d, want 4 (%+v)", len(chunks), chunks)
	}
	if chunks[0].Heading != "subsidios.md" || chunks[0].Text != "intro sin heading" {
		t.Errorf("chunk 0 = %+v", chunks[0])
	}
	if chunks[1].Heading != "Subsidios" || chunks[2].Heading != "Requisitos" || chunks[3].Heading != "FAQ" {
		t.Errorf("headings: %q %q %q", chunks[1].Heading, chunks[2].Heading, chunks[3].Heading)
	}
}

func TestKeywordRetrieve(t *testing.T) {
	r := &RAG{chunks: []Chunk{
		{Doc: "faq.md", Heading: "Cobertura de mascotas", Text: "El seguro de mascota cubre perro y gato."},
		{Doc: "faq.md", Heading: "Pagos", Text: "Puedes pagar mensual con débito."},
		{Doc: "glosario.md", Heading: "Prima", Text: "La prima es el valor mensual del seguro."},
	}}
	got := r.Retrieve(context.Background(), "¿qué cubre el seguro de mascotas?", 2)
	if len(got) == 0 || got[0].Heading != "Cobertura de mascotas" {
		t.Fatalf("retrieve = %+v", got)
	}
	if r.Mode() != "keyword" {
		t.Errorf("mode = %s, want keyword", r.Mode())
	}
	// consulta sin overlap: nada
	if got := r.Retrieve(context.Background(), "xyz zzz", 2); len(got) != 0 {
		t.Errorf("consulta sin match debe dar 0, dio %d", len(got))
	}
}

func TestCosine(t *testing.T) {
	if c := cosine([]float32{1, 0}, []float32{1, 0}); c < 0.999 {
		t.Errorf("cosine idéntico = %f", c)
	}
	if c := cosine([]float32{1, 0}, []float32{0, 1}); c != 0 {
		t.Errorf("cosine ortogonal = %f", c)
	}
	if c := cosine([]float32{}, []float32{1}); c != 0 {
		t.Errorf("cosine dims distintas = %f", c)
	}
}

func TestKeywordScoreWeighting(t *testing.T) {
	head := Chunk{Heading: "subsidios de vivienda", Text: "otro contenido"}
	body := Chunk{Heading: "otra sección", Text: "aplican subsidios según afiliación"}
	qw := keywords("¿tengo subsidios disponibles?")
	sh, sb := keywordScore(qw, head), keywordScore(qw, body)
	if sh <= 0 || sb <= 0 {
		t.Fatalf("scores = %f %f, ambos deben ser > 0", sh, sb)
	}
	if sh <= sb {
		t.Errorf("hit en heading (%f) debe pesar más que en body (%f)", sh, sb)
	}
}
