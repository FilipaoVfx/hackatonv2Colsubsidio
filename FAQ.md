# Preguntas frecuentes

### ¿Qué es Secura?

Un asesor de seguros conversacional para Colsubsidio. Atiende por WhatsApp y por
voz, califica al afiliado contra la API real de Colsubsidio Protege y entrega al
asesor humano un lead con contexto, no una transcripción.

### ¿Puedo probarlo sin instalar nada?

Sí: **[teamflashackaton30x.com/probar](https://teamflashackaton30x.com/probar)**.
Abre la CLI real en el navegador, contra el sistema real. Es de solo lectura.

### ¿Los números de la demo son reales?

Sí, y se puede verificar. Toda métrica lleva una insignia de procedencia:
◆ medido, ◈ derivado, ◇ simulado. `secura provenance --json` lista cada una con
su origen. La CLI **no compila** si intentas mostrar un número sin declarar de
dónde salió ([observabilidad](docs/observability.md)).

### ¿Usan Pinecone?

No. El corpus son cinco archivos Markdown, 103 líneas en total: se chunkean por
heading al arrancar, se embeben en una llamada batch y quedan en memoria. Con
ese tamaño, una base vectorial añadiría un punto de fallo sin aportar nada.
Argumentado en [ADR-0003](docs/adr/0003-rag-en-memoria.md), incluido en qué
momento la decisión dejaría de ser correcta.

### ¿Y Redis?

Tampoco. Las sesiones de WhatsApp se persisten por teléfono contra Supabase.

### ¿Por qué Go y no Python?

Un binario sin runtime, WebSocket nativo para el stream de eventos, y arranque
en milisegundos. El backend tiene **cuatro dependencias directas**; en Python el
mismo alcance trae un árbol de decenas. Ver
[ADR-0008](docs/adr/0008-monolito-go.md).

### ¿Por qué una CLI en vez de un dashboard web?

Porque en un hackathon un dashboard bonito es indistinguible de una maqueta.
Un pipeline de eventos avanzando en terminal, con latencias y costos reales, se
lee como sistema. Ver [ADR-0006](docs/adr/0006-cli-bubbletea.md).

### ¿Qué pasa si se cae Supabase?

El sistema **sigue conversando**. La persistencia degrada a memoria: se pierden
los eventos al reiniciar, no la conversación. Cada dependencia externa tiene un
modo degradado explícito, y `/api/capabilities` dice cuál está activo.

### ¿Es seguro exponerlo a internet?

**No como está.** El backend no tiene autenticación: cualquiera con la URL puede
llamar cualquier endpoint, incluidos los que escriben en producción. Hoy se
mitiga con URLs efímeras y con `--read-only` en la sesión pública de la CLI, que
no es lo mismo que resolverlo. El análisis completo, con lo que falta, está en
[seguridad](docs/security.md).

### ¿Cuánto cuesta cada conversación?

~$0,03 USD. Medido, no estimado: cada `LLM_RESPONSE` trae `cost_usd` del
proveedor. El acumulado del hackathon fue $1,29 sobre 452.298 tokens de entrada.

### ¿Por qué tantos tokens de entrada frente a los de salida?

30:1. Es la firma del tool calling: cada ciclo reenvía el contexto completo más
el resultado de la herramienta antes de que el modelo responda.

### ¿Cómo levanto el proyecto?

`make dev` y luego `make health`. Detalle en el [README](README.md#quick-start).

### ¿Los tests necesitan claves de API?

No. Las 22 suites corren sin red y sin secretos: los clientes externos están
sustituidos por dobles. Por eso 4.648 líneas de test tardan menos de un segundo.

### ¿Por qué el release solo es de Windows?

Porque todas las demás plataformas entran por el terminal web, que no depende
del sistema operativo y no exige instalar nada. Mantener binarios de macOS traía
además el bloqueo de Gatekeeper por firma.
