# Prompt engineering

Cómo se construye cada llamada al modelo, qué se puede cambiar sin tocar código
y qué protecciones existen. El código vive en
[`promptbuilder.go`](../guardian-ai/backend/promptbuilder.go).

## Principio

**Lo estable arriba, lo volátil abajo.** Las capas que no cambian entre turnos
van primero: benefician al caché de prompt del proveedor y hacen que la parte
variable quede cerca de la pregunta, donde el modelo le presta más atención.

Ver la jerarquía completa de ocho capas en [LLM](llm.md#jerarquía-del-prompt).

## Prompt base

Fijo, versionado, editable desde el Agent Studio (`GET /api/studio/prompt`).
Define identidad, alcance y lo que el agente no debe hacer.

No contiene secretos ni datos de cliente: extraerlo no da acceso a nada.

## Prompt dinámico

Se compone por turno con:

| Fuente | Qué aporta |
|---|---|
| Persona | Ocho perillas numéricas traducidas a instrucciones de tono |
| Estado del lead | En cuál de los 9 estados va la conversación |
| Variables capturadas | Lo que ya se sabe del afiliado, de `memory.go` |
| Contexto RAG | Chunks recuperados con su score |
| Herramientas | **Solo las permitidas en el estado actual** |
| Historial | Turnos previos |

## Variables de persona

Enteros, no texto libre. Esa es la decisión que hace versionable el tono:

| Variable | Efecto |
|---|---|
| `empathy` | Reconocimiento de la situación del afiliado |
| `formality` | Usted / tú, registro |
| `closeness` | Distancia con el interlocutor |
| `persuasion` | Cuánto empuja hacia el cierre |
| `proactivity` | Si propone sin que le pregunten |
| `emojis` | Densidad de emoji |
| `humor` | Ligereza permitida |
| `safety_level` | Cautela ante temas sensibles |

Un cambio de tono pasa por draft → publish → versión, y se puede revertir con
`rollback`. Una edición libre del prompt no deja ese rastro.

## Guardrails

Ordenados por lo que realmente protege:

**1. Herramientas restringidas por estado.** La más fuerte, porque es
estructural. En `AFFILIATION_CHECK` el modelo no tiene `create_enrollment` en el
conjunto disponible: no hay instrucción que convencerlo de saltárselo, porque no
está en la mesa.

**2. Los datos vienen de la API.** El modelo no puede inventar un producto ni un
precio: si no vino de Protege, no existe.

**3. `safety_level`** en la persona, versionado.

**4. Sin secretos en el prompt.**

## Lo que no está resuelto

- Sin sanitización del input antes de componer el prompt.
- Sin detección de intentos de inyección.
- Un atacante paciente probablemente puede desviar el tono o llevar al agente
  fuera de alcance. Lo que **no** puede es hacerle ejecutar acciones que su
  estado no permite.

Análisis completo en [seguridad](security.md#prompt-injection).

## Probar un cambio sin tocar producción

El Playground abre una sesión aislada contra un contenedor Protege distinto,
para que los usuarios de ensayo no se mezclen con los reales.

```bash
curl -sX PUT localhost:8099/api/studio/config/draft \
  -H 'Content-Type: application/json' -d '{"persona":{"formality":8}}'

curl -sX POST localhost:8099/api/studio/playground/start
curl -sX POST localhost:8099/api/studio/playground/message \
  -H 'Content-Type: application/json' -d '{"text":"hola"}'
```

Convence el resultado: `POST /api/studio/config/publish`.
No convence: `POST /api/studio/playground/reset` y el borrador nunca llegó a
producción.

## Ver también

- [LLM](llm.md) · [RAG](rag.md) · [API](api/README.md)
