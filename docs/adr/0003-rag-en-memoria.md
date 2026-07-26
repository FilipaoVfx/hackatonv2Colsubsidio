# ADR-0003 — RAG en memoria, sin base vectorial

## Estado

Aceptada.

## Contexto

El agente necesita responder con información de Colsubsidio: coberturas,
glosario de seguros, reglas de subsidio, preguntas frecuentes. Ese material
existe como texto curado a mano.

**Tamaño real del corpus: cinco archivos Markdown, 103 líneas en total.**

| Archivo | Líneas |
|---|---|
| `insights_afiliados.md` | 31 |
| `faq.md` | 27 |
| `glosario.md` | 19 |
| `productos.md` | 15 |
| `subsidios.md` | 11 |

Ese número es el contexto entero de esta decisión. La pregunta no era "qué base
vectorial usamos", sino "¿hace falta alguna?".

## Opciones consideradas

**A. Pinecone (o Weaviate, Qdrant gestionado).**
El reflejo por defecto de cualquier proyecto de RAG. Aporta: búsqueda ANN
escalable, filtrado por metadatos, persistencia del índice, actualización
incremental.
Cuesta: una dependencia externa más en el arranque, otra clave que rotar, otro
punto de fallo en la demo, latencia de red por consulta, y un índice que hay que
mantener sincronizado con el corpus.

**B. pgvector sobre el Supabase que ya usamos.**
Sin proveedor nuevo. Pero exige migración, mantener el índice al día y consultas
SQL para algo que resuelve un bucle de veinte líneas.

**C. Todo en memoria.**
Chunkear al arrancar, embeber en una llamada batch, guardar en un slice, coseno
por consulta.

## Decisión

**Opción C.** [`rag.go`](../../guardian-ai/backend/rag.go), 216 líneas:

1. Al arrancar, lee `KNOWLEDGE_DIR` y parte cada `.md` **por heading** — un
   corpus escrito a mano ya trae marcada su unidad semántica; trocear cada 512
   tokens cortaría una respuesta de FAQ por la mitad.
2. Embebe **todos** los chunks en **una sola** petición batch con
   `text-embedding-3-small`.
3. Los guarda en un slice en RAM.
4. Por consulta: embebe la query y hace coseno contra todo el slice.

Con decenas de chunks, el escaneo lineal es más rápido que cualquier índice ANN
— y **exacto** en vez de aproximado.

Si la llamada de embeddings falla, degrada a coincidencia por keyword y lo
registra. `RAG.Mode()` devuelve `"embeddings"` o `"keyword"`, y la CLI lo
muestra.

## Consecuencias

**A favor**

- **Cero infraestructura nueva.** No hay proveedor que se caiga durante el pitch,
  ni clave que rotar, ni índice que se desincronice.
- **Cero latencia de red** en la recuperación. El coseno sobre un slice es
  microsegundos frente a la ida y vuelta a un servicio externo.
- **Resultados exactos**, no aproximados.
- **Actualizar el corpus es editar un `.md` y reiniciar.** No hay comando de
  reindexado porque el arranque *es* el reindexado. Menos piezas, menos formas
  de que quede inconsistente.
- **Degrada de forma visible.** Sin clave de OpenAI el sistema sigue
  respondiendo, peor, y dice que está peor.

**En contra**

- **No escala.** Corpus completo en RAM y escaneo lineal por turno dejan de ser
  razonables mucho antes de las 10.000 secciones.
- **Sin persistencia de vectores**: cada reinicio recalcula embeddings. A este
  tamaño es despreciable; a otro, no.
- **Sin metadatos por chunk** más allá del heading: no hay fecha de vigencia, ni
  producto asociado, ni filtrado.
- **Sin actualización incremental.** Cambiar una línea obliga a re-embeber todo.
- **Sin versionado del corpus.** Git versiona los archivos, pero no queda
  registro de qué versión respondió cada conversación.
- **Sin reranking**: top-k por coseno y ya.

**Cuándo revisar**

Esta decisión deja de ser correcta cuando pase cualquiera de estas:

- El corpus supera ~500 chunks o ~1 MB de texto.
- Hace falta filtrar por metadatos (vigencia, línea de producto, región).
- El corpus se actualiza más rápido de lo que se puede reiniciar el servicio.
- Hay varios tenants con corpus distintos.

En ese punto la migración natural es **pgvector** sobre el Supabase existente
—no un proveedor nuevo—, porque la base ya está y el evento
`KNOWLEDGE_RETRIEVED` ya abstrae la recuperación: cambiar el motor no toca el
resto del sistema.

## Nota

Documentar "usamos Pinecone" habría quedado mejor en un pitch. Sería falso, y un
jurado técnico que abra `rag.go` lo descubre en treinta segundos. **La decisión
correcta para 103 líneas de corpus es no tener base vectorial**, y saber
explicar por qué vale más que la marca.

## Ver también

- [RAG](../rag.md) · [LLM](../llm.md)
