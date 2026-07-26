# ADR-0006 — CLI en Bubble Tea como interfaz de operación

## Estado

Aceptada.

## Contexto

El sistema necesitaba una interfaz para operarlo y para demostrarlo. La opción
obvia era un dashboard web, pero había un problema de credibilidad: en un
hackathon, un dashboard bonito es indistinguible de una maqueta. Cualquiera
puede pintar números en React.

Hacía falta algo que hiciera evidente que **hay un sistema real detrás**, y que
un jurado pudiera tocar en sesenta segundos.

## Opciones consideradas

**A. Dashboard web (React).**
Familiar y visual. Pero exige servidor, build y despliegue, y no distingue entre
datos reales y `const kpis = {...}`.

**B. CLI clásica con Cobra, salida plana.**
Honesta y trivial de escribir. Sin impacto visual: nadie recuerda una tabla en
stdout.

**C. TUI con Bubble Tea.**
Interfaz completa en terminal, con el modelo Elm de Charmbracelet.

## Decisión

**Opción C**, con Cobra debajo para los subcomandos no interactivos
(`doctor`, `tail`). Bubble Tea **v1**, no v2: `huh` v0.7 apunta a v1, y mezclar
rompe la interfaz `tea.Model`.

Ocho módulos, un `EventStream` sobre `/ws` alimentando a todos.

Dos decisiones estructurales que sostienen el resto:

1. **`Module.Update` devuelve `Module`, no `tea.Model`.** Elimina toda aserción
   de tipo del modelo raíz y mantiene `update.go` por debajo de 120 líneas.
2. **Todo número se muestra envuelto en `prov.Value[T]`**, que obliga a declarar
   si es medido, derivado o simulado. El widget de KPI **solo acepta ese tipo**:
   pintar una cifra sin procedencia no compila. Ver
   [observabilidad](../observability.md).

## Consecuencias

**A favor**

- **Credibilidad.** Un pipeline de eventos avanzando en una terminal, con
  latencias y costos reales, se lee como sistema, no como maqueta.
- Un binario sin dependencias que se distribuye por GitHub Releases, o se sirve
  en el navegador con `ttyd` sin instalar nada.
- La procedencia obligatoria hace **estructuralmente imposible** un número
  inventado — el problema típico de un dashboard de hackathon.
- `secura tail` da un `tail -f` del cerebro del sistema, útil de verdad para
  depurar.

**En contra**

- **Sin tests de la TUI.** Los seis defectos de render que aparecieron
  (desbordes de línea, orden de lista corrompido por el poll, stepper atascado)
  los encontró la inspección visual, no el compilador. `go build` no los ve.
- El envelope `Event` está **duplicado** en el módulo de la CLI: el backend es
  `package main` y sus tipos no son importables. Cambiarlo en un lado sin el
  otro rompe en ejecución.
- El renderizado en terminal tiene trampas propias: `lipgloss.Width()` hace
  word-wrap donde se esperaba truncado, y una línea larga desplaza la cabecera
  fuera de pantalla. Hubo que truncar con `ansi.Truncate` a ancho exacto.
- Público limitado: quien no vive en la terminal prefiere una web.

**Cuándo revisar**

Para operación por usuarios de negocio hace falta una web. La CLI seguiría
siendo la herramienta de ingeniería — no se reemplazan, se suman.

## Ver también

- [Observabilidad](../observability.md) · [ADR-0007](0007-guarda-read-only.md)
