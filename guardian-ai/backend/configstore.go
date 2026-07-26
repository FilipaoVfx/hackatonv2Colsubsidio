package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfigStore guarda la configuración del Agent Studio (plan
// 10_PLAN_AGENT_STUDIO.md §5). Fuente de verdad: un archivo JSON en el volumen
// del backend. Réplica opcional en Postgres cuando hay pool — mismo contrato
// que persist.go: la base es opcional y su caída nunca es fatal.
//
// Degradación honesta: sin archivo se arranca con los defaults de fábrica; con
// un archivo ilegible se arranca con defaults y se recuerda el error para que
// la API lo exponga (store_error). El motor nunca arranca con una
// configuración a medias, y nunca hace panic por esto.

// configDir resuelve dónde vive el archivo (CONFIG_DIR, por defecto el volumen
// del contenedor). En local, sin la variable, escribe junto al binario.
func configDir() string {
	if d := os.Getenv("CONFIG_DIR"); d != "" {
		return d
	}
	return "/var/lib/guardian"
}

const configFileName = "agent_config.json"

// configFile es el formato en disco: lo publicado (lo que corre), el borrador
// (lo que se está diseñando) y el historial de versiones publicadas.
type configFile struct {
	Published *AgentConfig  `json:"published"`
	Draft     *AgentConfig  `json:"draft"`
	History   []AgentConfig `json:"history"`
}

type ConfigStore struct {
	mu   sync.Mutex
	path string
	pool *pgxpool.Pool // opcional

	published AgentConfig
	draft     AgentConfig
	history   []AgentConfig
	loadErr   string // vacío = carga limpia
}

// NewConfigStore carga el estado del disco. Nunca devuelve error: un fallo de
// lectura degrada a los defaults y queda registrado en LoadError().
func NewConfigStore(dir string, pool *pgxpool.Pool) *ConfigStore {
	s := &ConfigStore{
		path:      filepath.Join(dir, configFileName),
		pool:      pool,
		published: DefaultConfig(),
		draft:     DefaultConfig(),
	}
	s.draft.Status = "draft"

	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s // primera ejecución: defaults de fábrica
	}
	if err != nil {
		s.loadErr = "no se pudo leer la configuración: " + err.Error()
		log.Printf("agent studio: %s (se arranca con defaults)", s.loadErr)
		return s
	}

	var file configFile
	if err := json.Unmarshal(raw, &file); err != nil {
		s.loadErr = "configuración ilegible: " + err.Error()
		log.Printf("agent studio: %s (se arranca con defaults)", s.loadErr)
		return s
	}
	// Una configuración guardada por una versión anterior podría no cumplir las
	// reglas de hoy: se valida al cargar y, si no cumple, se ignora en vez de
	// alimentar al motor con algo inválido.
	if file.Published != nil {
		if errs := file.Published.Validate(); len(errs) > 0 {
			s.loadErr = "configuración publicada inválida: " + errs[0].Error()
			log.Printf("agent studio: %s (se arranca con defaults)", s.loadErr)
		} else {
			s.published = file.Published.Clone()
		}
	}
	if file.Draft != nil && len(file.Draft.Validate()) == 0 {
		s.draft = file.Draft.Clone()
	} else {
		s.draft = s.published.Clone()
		s.draft.Status = "draft"
	}
	s.history = append(s.history, file.History...)
	return s
}

// Published devuelve una copia de la configuración viva.
func (s *ConfigStore) Published() AgentConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.published.Clone()
}

// Draft devuelve una copia del borrador en diseño.
func (s *ConfigStore) Draft() AgentConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draft.Clone()
}

// History devuelve las versiones publicadas, de la más reciente a la más vieja.
func (s *ConfigStore) History() []AgentConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AgentConfig, 0, len(s.history))
	for i := len(s.history) - 1; i >= 0; i-- {
		out = append(out, s.history[i].Clone())
	}
	return out
}

// LoadError informa de un arranque degradado (vacío si todo fue bien).
func (s *ConfigStore) LoadError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// SaveDraft valida y guarda el borrador. Devuelve los errores por campo cuando
// la configuración no es válida (el borrador anterior queda intacto).
func (s *ConfigStore) SaveDraft(cfg AgentConfig) (AgentConfig, []FieldError, error) {
	cfg.Normalize()
	if errs := cfg.Validate(); len(errs) > 0 {
		return AgentConfig{}, errs, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = cfg.Clone()
	cfg.Status = "draft"
	cfg.Version = s.published.Version // el borrador hereda la versión viva hasta publicarse
	cfg.UpdatedAt = time.Now().UTC()
	s.draft = cfg
	if err := s.persistLocked(); err != nil {
		return AgentConfig{}, nil, err
	}
	return s.draft.Clone(), nil, nil
}

// Publish promueve el borrador a configuración viva con una versión nueva.
// Guarda ANTES de que el llamador la aplique al motor: lo que corre siempre es
// lo que quedó en disco, nunca algo que no se pudo persistir.
func (s *ConfigStore) Publish(note string) (AgentConfig, []FieldError, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishLocked(s.draft, note)
}

// Restore vuelve a publicar una versión anterior. No reescribe la historia: la
// versión recuperada entra como una versión NUEVA, así el historial sigue
// contando lo que de verdad pasó y se puede deshacer el deshacer.
func (s *ConfigStore) Restore(version int, note string) (AgentConfig, []FieldError, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var source *AgentConfig
	if s.published.Version == version {
		source = &s.published
	}
	for i := range s.history {
		if s.history[i].Version == version {
			source = &s.history[i]
			break
		}
	}
	if source == nil {
		return AgentConfig{}, nil, fmt.Errorf("la versión %d no está en el historial", version)
	}
	if note == "" {
		note = fmt.Sprintf("vuelta a la versión %d", version)
	}
	return s.publishLocked(*source, note)
}

// publishLocked es el camino común de publicar y recuperar: validar, versionar,
// apilar la anterior, persistir y — solo si el disco aceptó — dejarla viva.
func (s *ConfigStore) publishLocked(source AgentConfig, note string) (AgentConfig, []FieldError, error) {
	cfg := source.Clone()
	cfg.Note = note
	cfg.Normalize()
	if errs := cfg.Validate(); len(errs) > 0 {
		return AgentConfig{}, errs, nil
	}

	previous := s.published.Clone()
	cfg.Version = s.published.Version + 1
	cfg.Status = "published"
	cfg.UpdatedAt = time.Now().UTC()

	s.history = append(s.history, previous)
	if len(s.history) > MaxConfigHistory {
		s.history = s.history[len(s.history)-MaxConfigHistory:]
	}
	s.published = cfg
	s.draft = cfg.Clone()
	s.draft.Status = "draft"

	if err := s.persistLocked(); err != nil {
		// Rollback en memoria: si no se pudo guardar, no se publica.
		s.published = previous
		s.history = s.history[:len(s.history)-1]
		return AgentConfig{}, nil, err
	}
	s.replicate(cfg)
	return s.published.Clone(), nil, nil
}

// persistLocked escribe el archivo de forma atómica (temporal + rename) para
// que un corte a media escritura no deje una configuración truncada.
func (s *ConfigStore) persistLocked() error {
	published, draft := s.published, s.draft
	body, err := json.MarshalIndent(configFile{
		Published: &published, Draft: &draft, History: s.history,
	}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent_config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op si el rename ya lo movió
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// replicate copia la versión publicada a Postgres cuando hay pool. Best-effort:
// un fallo se registra y no afecta a la publicación (el archivo ya es la
// fuente de verdad).
func (s *ConfigStore) replicate(cfg AgentConfig) {
	if s.pool == nil {
		return
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.pool.Exec(ctx, `
			create table if not exists public.agent_configs (
				version    int primary key,
				status     text not null,
				note       text,
				config     jsonb not null,
				created_at timestamptz not null default now()
			)`); err != nil {
			log.Printf("agent studio: no se pudo preparar agent_configs: %v", err)
			return
		}
		if _, err := s.pool.Exec(ctx,
			`insert into public.agent_configs (version, status, note, config)
			 values ($1,$2,$3,$4) on conflict (version) do nothing`,
			cfg.Version, cfg.Status, cfg.Note, body); err != nil {
			log.Printf("agent studio: réplica en Postgres falló (v%d): %v", cfg.Version, err)
		}
	}()
}
