# 10 — Plan de implementación: Agent Studio

> Plan de trabajo para el PRD `agentStudio.md`. Documento de diseño previo a la
> implementación: define qué se construye, qué NO, y sobre todo **qué no puede
> tocarse** para no perder la robustez ganada en `09_FEATURE_ROBUSTEZ_FLUJO_BOT.md`.

## 1. Contexto

El asesor Guardian se comporta hoy según constantes en código: la persona vive en
`backend/promptbuilder.go:46`, los objetivos por etapa en `statemachine.go:StateGoal`,
los mensajes fijos en `guardian.go`, y los parámetros del modelo en `openai.go:20`.
Cambiar el tono, el objetivo comercial o una prohibición exige editar Go, recompilar
y redesplegar. El PRD pide lo contrario: que una persona de negocio **diseñe el
comportamiento** con controles visuales y publique, sin escribir prompts.

La dificultad no es la UI: es que esa configuración entra en un motor que acaba de
ser endurecido contra nueve fallos reales (carreras entre turnos, reentregas del
webhook, saltos de etapa, callejones sin salida, whitelist saltable, variables
inventadas, perfil estimado tomado por confirmado, caché que cacheaba su fallo,
fugas de sesión). Una configuración editable es una **nueva superficie de entrada
no confiable** apuntando al corazón del motor. El plan trata la robustez como el
requisito primario y la configurabilidad como lo que se construye encima.

**Decisiones tomadas con el usuario** (2026-07-25):
- **UI vanilla** sobre `guardian.css`, servida por nginx como `/chat` y `/pipeline`.
  Nada de React/Vite/shadcn: un solo mundo visual (el de `DESIGN.md`) y cero build nuevo.
- **Alcance real del MVP**: Personality + Sales + Safety, y Playground + Prompt Inspector.
  El resto se muestra como estado real de solo lectura o como maqueta declarada.
- **Publicar aplica al siguiente turno de todas las conversaciones**, con la versión
  estampada por turno.
- **Persistencia**: archivo JSON como fuente de verdad + réplica en Supabase cuando
  `SUPABASE_DB_URL` exista (mismo contrato opcional que `persist.go`).

## 2. Principio rector: qué puede y qué no puede cambiar una configuración

Esta tabla es el contrato de seguridad de la feature. Si un control del Studio no
cabe en la columna izquierda, no se construye como control: se muestra como estado.

| Configurable (con rango validado) | Intocable desde el Studio (y por qué) |
|---|---|
| Persona: empatía, formalidad, cercanía, persuasión, proactividad (1–10) | Flechas legales de la máquina de estados (`leadTransitions`) — el embudo es el producto |
| Longitud de respuesta (breve/media/detallada), emojis, humor | Whitelist de acciones por etapa y `FallbackAction` — fallo 5 del informe de robustez |
| Nombre visible del agente (≤40 chars, sin saltos ni `#`) | Vocabulario cerrado de variables (`acceptedKeys`) — fallo 6 |
| Objetivos comerciales y su orden (catálogo cerrado de 6) | Que la **API decide** elegibilidad, reglas y recomendación (RN-001/RN-005) |
| Prohibiciones de seguridad (catálogo cerrado) y nivel de protección | Structured outputs estrictos y el enum de `intent` |
| — (fase posterior) ventana de memoria, top-k y umbral de RAG | Persistencia inmediata de hechos confirmados y umbral de confianza 0.6 |
| — (fase posterior) temperatura y modelo | Serialización por teléfono, deduplicación del webhook, barrido de sesiones |

Consecuencia de diseño: **el prompt generado se compone de piezas cerradas**. El único
texto libre que llega al modelo es el nombre del agente, saneado. No hay campo donde
escribir instrucciones — es literalmente lo que pide el PRD ("sin prompt engineering")
y a la vez la defensa contra inyección por configuración.

## 3. Modelo de configuración

Archivo nuevo `backend/agentconfig.go` — structs + validación + mapeo a prompt, todo
funciones puras (mismo estilo que `promptbuilder.go`, testeable sin red).

```go
type AgentConfig struct {
    Version   int       // monotónico; 0 = defaults de fábrica
    Status    string    // "draft" | "published"
    Note      string    // nota de versión (≤120 chars)
    UpdatedAt time.Time

    Persona PersonaConfig
    Sales   SalesConfig
    Safety  SafetyConfig
    Runtime RuntimeConfig // MVP: solo lectura, refleja el runtime real
}

type PersonaConfig struct {
    AgentName   string // default "Guardian"
    Empathy     int    // 1..10  (default 8)
    Formality   int    // 1..10  (default 5)
    Closeness   int    // 1..10  (default 7)
    Persuasion  int    // 1..10  (default 4)
    Proactivity int    // 1..10  (default 6)
    Length      string // "breve" | "media" | "detallada" (default "media")
    Emojis      bool   // default true
    Humor       bool   // default false
}

type SalesConfig struct {
    Goals []string // orden = prioridad; subconjunto del catálogo cerrado
}

type SafetyConfig struct {
    Forbid []string // subconjunto del catálogo cerrado
    Level  string   // "bajo" | "medio" | "alto" (default "alto")
}

type RuntimeConfig struct { // expuesto para la UI; NO editable en el MVP
    Model, RAGMode                string
    Temperature                   float64
    HistoryWindow, RAGTopK        int
    ConfidenceThreshold           float64
}
```

Catálogos cerrados (constantes en Go, servidos a la UI por la API para que no se
dupliquen literales en el frontend):
- `salesGoals`: resolver_dudas, calificar_cliente, recomendar_producto, cerrar_venta,
  agendar_llamada, derivar_humano.
- `safetyForbid`: coberturas_inventadas, promesas_falsas, consejos_legales,
  consejos_medicos, informacion_inexistente.

`Validate() []FieldError` — enteros fuera de rango se rechazan (no se recortan en
silencio), enums verificados, catálogos verificados, `AgentName` saneado, `Goals` sin
duplicados y no vacío. Devuelve errores **por campo** para que la UI los muestre donde
corresponde. **`DefaultConfig()` debe producir exactamente el comportamiento de hoy**:
ese es el criterio de aceptación de la fase 0.

## 4. Cómo entra la configuración al motor sin abrir carreras

El motor ya paga el precio de la concurrencia una vez (`keyedMutex` por teléfono).
La configuración no puede reintroducir el problema.

- `GuardianEngine` gana `cfg atomic.Pointer[AgentConfig]` y `SetConfig(*AgentConfig)`.
- `turn()` (en `guardian.go`) lee **un snapshot inmutable al empezar**:
  `cfg := e.config()`. Todo el turno usa ese puntero. Publicar durante un turno no lo
  altera: el turno siguiente ya verá la versión nueva.
- Las configuraciones nunca se mutan tras publicarse; publicar crea una copia nueva y
  cambia el puntero. Sin locks en la ruta caliente.
- `BuildSystemPrompt(PromptInput{... , Config: cfg})` — la firma existente crece con un
  campo; las secciones siguen siendo puras.
- Trazabilidad: `LLM_REQUESTED` y `TURN_COMPLETED` estampan `config_version`. Con eso,
  cualquier conversación del pipeline dice con qué configuración se comportó el bot.
- `GuardianLLM.DecideGuardian` gana un parámetro `opts LLMOptions{Model, Temperature}`
  con cero-valor = comportamiento actual. Los dobles de test (`scriptedLLM`, `slowLLM`)
  lo ignoran; el cambio de firma es mecánico y deja lista la fase de perillas de modelo.

## 5. Almacenamiento y versionado

Archivo nuevo `backend/configstore.go`.

- **Fuente de verdad**: `${CONFIG_DIR:-/data}/agent_config.json`, con
  `{"published": {...}, "draft": {...}, "history": [...]}`. Escritura atómica
  (temporal + `rename`) bajo mutex; lectura completa al arrancar.
- **Réplica opcional** en Postgres cuando hay pool (`persist.go:Pool()`):
  `public.agent_configs(version int primary key, status text, note text, config jsonb, created_at timestamptz)`.
  Best-effort: un fallo se registra y nunca es fatal — mismo contrato que los eventos.
- **Arranque degradado honesto**: sin archivo → defaults de fábrica; archivo ilegible →
  defaults + log de error visible en `/api/studio/config` como `store_error`. Nunca panic,
  nunca arrancar con una configuración a medias.
- Publicar = `version+1`, `status=published`, se apila el anterior en `history` (últimas
  20), se persiste y **después** se aplica con `SetConfig`. Si la escritura falla, no se
  aplica: lo que corre siempre es lo que quedó guardado.
- El volumen ya existe en `docker-compose.yml`; se añade el montaje de `./data` si no está.

## 6. Del control visual al prompt (mapeo determinista)

En `agentconfig.go`, funciones puras que traducen números a instrucciones. Sin
interpolar valores crudos ("empatía = 8" no significa nada para el modelo): cada rango
tiene su frase.

| Control | 1–3 | 4–7 | 8–10 |
|---|---|---|---|
| Empatía | "Ve al grano. Reconoce emociones solo si la persona las expresa." | "Reconoce brevemente cómo se siente antes de avanzar." | "Nombra la emoción de la persona y valida su preocupación antes de cualquier dato." |
| Formalidad | "Tutea, lenguaje coloquial colombiano." | "Tutea con registro profesional." | "Trato de usted, registro corporativo, sin coloquialismos." |
| Cercanía | "Distancia profesional; sin comentarios personales." | "Cordial; puedes referirte a lo que la persona contó." | "Cálido y personal; retoma detalles que compartió antes." |
| Persuasión | "Rol consultor: informa y deja decidir." | "Sugiere el siguiente paso una vez por conversación." | "Propón activamente el cierre cuando el perfil lo justifique, sin presionar." |
| Proactividad | "Responde lo preguntado; no abras temas." | "Propón el siguiente tema cuando el actual esté resuelto." | "Lleva tú la conversación proponiendo el siguiente paso en cada turno." |

- **Longitud** → límite explícito de frases (breve 1–2, media 2–4, detallada 4–6), que
  además se verifica en la validación previa a publicar.
- **Emojis / humor** → una línea permisiva o prohibitiva; por defecto, como hoy.
- **Sales** → sección nueva "Objetivos por prioridad" numerada; el primero manda cuando
  hay tensión. No sustituye a `StateGoal`: lo complementa, porque el objetivo de etapa
  es el que sostiene la máquina de estados.
- **Safety** → líneas en "Reglas de conversación" (ya existe la regla de no inventar) +
  el nivel: `alto` añade "ante cualquier duda, deriva a un asesor humano".

**Prueba de oro (fase 0)**: `BuildSystemPrompt` con `DefaultConfig()` produce un prompt
**idéntico** al actual. Cualquier diferencia rompe el test. Es la garantía de que
introducir el Studio no cambia, por sí solo, el comportamiento del bot en producción.

## 7. Playground aislado (la pieza que no puede tocar producción)

Requisito duro: probar el borrador **jamás** debe enviar un WhatsApp real, contaminar
el pipeline, ni interferir con conversaciones vivas.

Aislamiento por construcción, no por bandera:

| Recurso | Producción | Playground |
|---|---|---|
| Event bus | `bus` (persistencia + hub + entrega Kapso suscritos) | `studioBus` nuevo: **nadie** suscribe entrega ni persistencia |
| WebSocket | `/ws` | `/ws/studio` (hub propio) |
| Sesiones | `sessions` | `studioSessions` propio |
| API Protege | `COLSUBSIDIO_API_URL` | `STUDIO_API_URL`, por defecto el **mock** (`http://mock-protege:9000`) |
| Configuración | publicada | **borrador** |

El consumidor de `MESSAGE_SENT` que entrega por Kapso vive en el bus principal
(`main.go:147`), así que un mensaje del playground no tiene camino físico hacia
WhatsApp. La persistencia también está suscrita solo al bus principal, así que
`/api/calls`, el pipeline y las analíticas no ven nada del Studio. Todo esto se prueba
con un test, no con confianza.

Endpoints:
- `POST /api/studio/playground/start` → abre sesión sandbox con teléfono sintético.
- `POST /api/studio/playground/message` `{text}` → un turno con el borrador.
- `POST /api/studio/playground/reset` → cierra y limpia.
- `GET /ws/studio` → eventos en vivo para el panel derecho.

Límites: máximo 20 turnos por sesión y expiración por inactividad (reusa
`WhatsAppSessions.Sweep()`), para que una pestaña olvidada no queme tokens.
El coste real del turno (`cost_usd`, `latency_ms` de `LLM_RESPONSE`) se muestra en la UI
— el Studio es también el sitio honesto para ver lo que cuesta cada cambio.

## 8. API del Studio

Todas bajo `/api/studio` en `main.go`, junto a las rutas existentes.

| Método | Ruta | Propósito |
|---|---|---|
| GET | `/api/studio/config` | `{published, draft, defaults, catalogs, runtime, store_error}` |
| PUT | `/api/studio/config/draft` | Valida y guarda borrador. `422` con errores por campo |
| POST | `/api/studio/config/publish` | `{note}` → valida, versiona, persiste, aplica. Devuelve versión |
| GET | `/api/studio/versions` | Historial para la línea de tiempo |
| POST | `/api/studio/config/rollback/:version` | Republica una versión anterior (fase 4) |
| GET | `/api/studio/prompt` | Prompt generado, solo lectura (`?draft=1&state=PROFILE_DISCOVERY`) |

`GET /api/studio/prompt` reutiliza `BuildSystemPrompt` con datos representativos
(catálogo real de la API + memoria de ejemplo), así que el Prompt Inspector muestra
**el prompt de verdad**, no una aproximación.

## 9. Interfaz `/studio`

`frontend/studio.html` + `frontend/studio.js`, sirviendo con la ruta nueva en
`nginx.conf` (`location = /studio { try_files /studio.html =404; }`). El ítem
"Configuración" del `nav` deja de ser `data-soon` en las cuatro vistas.

Reutiliza el sistema existente (`guardian.css`): `.shell`, `.sidebar`, `.nav`, `.cred`,
`.field`, `.label`, `.seal`, `.chip`, `.btn-primary`, `.empty-state`, `.skeleton`.
Los sliders y switches se añaden como componentes nuevos del mismo lenguaje (regla
punteada + valor en mono a la derecha, como los campos de la credencial), no como
controles genéricos de librería.

| Sección | Estado en el MVP |
|---|---|
| General | **Real**: nombre, estado (borrador/publicado), versión, modelo, última actualización, botón Publicar |
| Personality | **Real**: 5 sliders + longitud + 2 switches, con preview inmediato |
| Sales | **Real**: objetivos con orden por arrastre o botones ↑/↓ (teclado incluido) |
| Safety | **Real**: checklist de prohibiciones + nivel |
| Playground | **Real**: chat contra el borrador, con escenarios rápidos y coste por turno |
| Prompt Inspector | **Real**: prompt generado, solo lectura, con copiar |
| Knowledge, Memory, Reasoning | **Solo lectura honesta**: muestran los valores vigentes del motor (documentos del corpus, modo RAG, ventana de historial, tools registradas) con la etiqueta "no editable en esta versión". Nada de interruptores muertos |
| Voice, Channels, Experiments, Analytics | **Maqueta declarada** con distintivo visible, según el PRD |

Preview de personalidad: pares de respuesta precalculados por bucket (no llama al LLM en
cada arrastre del slider); el chat real está en el Playground, a un clic. Así se cumple
"cada cambio se ve al instante" sin una llamada por movimiento del dedo.

Accesibilidad: `input[type=range]` reales con `aria-valuetext` (la frase, no el número),
foco visible con el anillo del sistema, contraste AA, orden de tabulación por sección.

## 10. Fases, con criterio de aceptación

| Fase | Contenido | Se acepta cuando |
|---|---|---|
| **F0 — Cimientos invisibles** | `agentconfig.go`, `configstore.go`, snapshot atómico, `config_version` en eventos, montaje de `/data` | Prueba de oro: prompt con defaults **idéntico** al actual; `go vet` limpio y `go test -race ./...` verde; el bot se comporta igual (cero diferencia observable) |
| **F1 — Lectura** | `GET /api/studio/config`, `GET /api/studio/prompt`, página General + Prompt Inspector | El Inspector muestra el prompt real; ninguna ruta de escritura existe todavía |
| **F2 — Diseño del comportamiento** | Personality, Sales, Safety → prompt; `PUT draft` con validación | Tests de mapeo por bucket; configuraciones inválidas devuelven `422` por campo; el prompt cambia como se espera |
| **F3 — Playground** | Motor, bus, sesiones y hub aislados; `/ws/studio`; escenarios | Test de aislamiento: un turno de playground no produce entrega Kapso, no persiste y no aparece en `/api/calls`; conversaciones vivas intactas |
| **F4 — Publicar** | Versionado, publicación atómica, historial, rollback, validación previa | Test con `-race` publicando mientras corren turnos; `config_version` visible por turno; rollback restaura comportamiento |
| **F5 — Cierre** | Maquetas declaradas, `10_FEATURE_AGENT_STUDIO.md`, catálogo de eventos, tabla antes/después | Documentación completa y honesta; `go test -race` verde de punta a punta |

Cada fase entra en su propio commit, con la trazabilidad del informe de robustez
(qué fallo/riesgo cubre, qué test lo prueba).

## 11. Riesgos y mitigación

| Riesgo | Mitigación |
|---|---|
| Una configuración desactiva una invariante del motor | Catálogos cerrados + `Validate()`; test explícito: **ninguna** configuración válida puede quitar la whitelist de acciones, saltarse la API ni ampliar el vocabulario de variables |
| Inyección de instrucciones por campos de texto | Único texto libre en el prompt: nombre del agente, ≤40 chars, sin saltos ni `#`. La nota de versión nunca entra al prompt |
| Prompt inflado (detallada + 6 objetivos + prohibiciones) sube coste y latencia | Cota de tamaño verificada al publicar (≤8 KB) y coste por turno visible en el Playground |
| Publicar en mitad de la demo cambia el bot sin aviso | Snapshot por turno + `config_version` estampada + aviso visible en el Studio de qué versión está viva |
| Configuración corrupta en disco | Arranque con defaults, `store_error` expuesto en la API, sin panic |
| El Playground quema tokens | Tope de 20 turnos por sesión, expiración por inactividad, coste a la vista |
| El Studio se percibe como maqueta | Las secciones no implementadas se declaran como tales; las de solo lectura muestran valores reales del motor |

## 12. Cómo verificar

```bash
cd guardian-ai

# Suite completa con detector de carreras (incluye los tests nuevos del Studio)
docker run --rm -m 1500m -v "$PWD/backend":/src -w /src -e GOFLAGS=-p=1 \
  golang:1.22-alpine sh -c 'apk add --no-cache gcc musl-dev >/dev/null; go test -race ./...'

# Prueba de oro del prompt por defecto y mapeo de personalidad
... -run "DefaultConfig|PromptFrom|StudioIsolation|Publish" -v

# Extremo a extremo
curl -s localhost:8099/api/studio/config | python3 -m json.tool
curl -s -X PUT localhost:8099/api/studio/config/draft -H 'Content-Type: application/json' \
  -d '{"persona":{"empathy":2,"length":"breve","emojis":false}}'
curl -s "localhost:8099/api/studio/prompt?draft=1" | head -40
curl -s -X POST localhost:8099/api/studio/playground/start
curl -s -X POST localhost:8099/api/studio/playground/message -d '{"text":"hola, esto es caro"}'
curl -s -X POST localhost:8099/api/studio/config/publish -d '{"note":"más directo"}'

# Aislamiento: tras usar el playground, nada nuevo aquí
curl -s localhost:8099/api/calls
```

En la UI: abrir `/studio`, bajar empatía a 2 y longitud a breve, ver el preview cambiar,
abrir el Playground y comprobar el tono, revisar el Prompt Inspector, publicar y
verificar que el siguiente turno de una conversación real ya trae la versión nueva en
`TURN_COMPLETED.config_version`.

## 13. Registro de avance

| Fase | Estado | Qué quedó |
|---|---|---|
| **F0 — Cimientos invisibles** | ✅ 2026-07-25 | `agentconfig.go` (modelo, catálogos cerrados, defaults, validación por campo), `configstore.go` (archivo atómico + réplica opcional en Postgres, degradación honesta), snapshot atómico en `GuardianEngine` (`SetConfig`/`Config`), `config_version` estampada en `LLM_REQUESTED` y `TURN_COMPLETED`, volumen `guardian-config` en compose. **Cero cambio de comportamiento**: la prueba de oro `TestPromptDefaultGolden` congela el prompt actual en `backend/testdata/prompt_default.golden` |
| F1 — Lectura | pendiente | |
| F2 — Diseño del comportamiento | pendiente | |
| F3 — Playground | pendiente | |
| F4 — Publicar | pendiente | |
| F5 — Cierre | pendiente | |

Nota de F0: `TestPublishDuringTurnsIsRaceFree` usa un solo teléfono a propósito.
La API de pruebas devuelve siempre la misma `conversation_id`, así que varios
teléfonos compartirían `guardianConv` y el candado por teléfono no los
serializaría — en producción cada conversación tiene su id. El paralelismo entre
clientes ya lo cubre `TestGuardianConcurrentInboundSerialized`.

## 14. Fuera de alcance (roadmap declarado)

Multiusuario, roles, auditoría completa, A/B real, métricas de producción, LangSmith,
gestión de secretos, marketplace, edición manual del prompt, publicación programada,
aprobaciones, CI/CD de prompts — tal como los excluye el PRD. Se suman a esa lista, por
decisión de este plan: perillas editables de Knowledge/Memory/Reasoning (se muestran
como estado real), configuración de voz, personalidad por canal y analíticas reales.
