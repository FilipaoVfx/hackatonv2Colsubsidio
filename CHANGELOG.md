# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/).
Versionado [SemVer](https://semver.org/lang/es/).

## [No publicado]

### Añadido
- Documentación completa en `docs/`: arquitectura, API, LLM, RAG, seguridad,
  observabilidad, testing y despliegue, con diagramas Mermaid
- Ocho ADR de las decisiones reales del proyecto
- `Makefile` con `dev`, `test`, `lint`, `health`, `docker`, `clean`
- README de módulo en `guardian-ai/backend`
- `CONTRIBUTING.md`, `ROADMAP.md`, `FAQ.md`, `LICENSE`
- Plantillas de issue y PR, y workflow de CI

### Corregido
- **`go.sum` del backend nunca se había commiteado**: un clon limpio no
  compilaba ni podía correr los tests

### Cambiado
- Raíz del repositorio reorganizada: los 12 documentos sueltos pasan a
  `docs/{specs,architecture,reference,design}`, los binarios y muestras a
  `assets/`

## [cli-v0.1.0] — 2026-07-26

### Añadido
- **Secura CLI**: centro de operaciones en terminal con ocho módulos
- Flag `--read-only` con guarda en la capa de datos ([ADR-0007](docs/adr/0007-guarda-read-only.md))
- Descubrimiento del backend desde `teamflashackaton30x.com/secura-endpoint.json`
- Release de Windows con verificación SHA-256 e instalador PowerShell
- Terminal web por `ttyd`: probar la CLI sin instalar nada
- Sección "Probar" en la landing

### Corregido
- La barra de estado quedaba flotando a media pantalla en terminales altos
- El listado de conversaciones se desordenaba con el poll de 2 s
- El stepper del pipeline se quedaba en `NEW` para usuarios recurrentes
- `TOOL_CALLED` leía `name` en vez de `tool` en el payload
- Tokens y costo se leían de `TURN_COMPLETED` en vez de `LLM_RESPONSE`
- Los módulos no activos nunca recibían sus propias respuestas asíncronas
