# Roadmap

## v1 — Hackathon ✅

Lo que existe hoy y funciona contra el sistema real.

- Motor conversacional con máquina de 9 estados
- Canal WhatsApp (Kapso) y voz (Vapi + ElevenLabs)
- RAG sobre corpus curado, con degradación a keyword
- Tool calling contra la Colsubsidio Protege API
- Event sourcing con proyecciones en Supabase
- Agent Studio: persona versionada con publish y rollback
- Secura CLI: ocho módulos, procedencia obligatoria en cada métrica
- Landing desplegada + terminal web sin instalación

## v2 — Panel del asesor

El lead calificado tiene que llegarle a un humano.

- [ ] **Autenticación** en los endpoints de escritura — hoy no hay ninguna
      ([seguridad](docs/security.md))
- [ ] **Rate limiting**: nada impide agotar el presupuesto de tokens
- [ ] Bandeja del asesor: leads en `READY_FOR_ADVISOR` con contexto completo
- [ ] Handoff explícito de agente a humano dentro de la conversación
- [ ] Herramienta de migraciones para el esquema ([ADR-0002](docs/adr/0002-supabase-event-store.md))
- [ ] Política de retención de datos personales

## v3 — Voz en producción

- [ ] Número propio con verificación de negocio (salir del sandbox de Kapso)
- [ ] Latencia de voz por debajo del segundo
- [ ] Interrupciones y turnos naturales
- [ ] Correlación entre canales: hoy WhatsApp y voz son `call_id` sin vínculo

## v4 — Analítica

- [ ] Panel de conversión por segmento y por producto
- [ ] Alertas de costo — hoy nadie se entera si se dispara
- [ ] A/B de personas y prompts, aprovechando el versionado que ya existe
- [ ] Snapshots del log de eventos para acotar el crecimiento

## v5 — Marketplace de agentes

- [ ] Multi-tenant: un agente por línea de negocio
- [ ] Corpus por tenant — el punto donde el
      [RAG en memoria](docs/adr/0003-rag-en-memoria.md) deja de servir y toca
      migrar a pgvector
- [ ] Constructor visual de flujos
- [ ] Marketplace de herramientas

## Deuda técnica reconocida

Ordenada por lo que más duele:

1. **`projector.go` con 884 líneas** — cada evento nuevo lo toca
2. **El envelope `Event` duplicado** entre backend y CLI: extraer `pkg/events`
   ([ADR-0008](docs/adr/0008-monolito-go.md))
3. **Sin tests de la TUI** — los defectos de render los encuentra el ojo
4. **Sin migraciones de esquema**
5. **Sin contract test** que detecte la deriva del envelope
6. `guardian.go` con 1.048 líneas pide separación
