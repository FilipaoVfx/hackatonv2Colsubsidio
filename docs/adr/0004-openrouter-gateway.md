# ADR-0004 — OpenRouter como gateway de LLM

## Estado

Aceptada.

## Contexto

El agente necesita un modelo capaz de conversar en español colombiano, seguir
una máquina de estados y usar herramientas con fiabilidad. Durante el desarrollo
había que poder comparar modelos sin reescribir el cliente.

## Opciones consideradas

**A. Anthropic directo.**
Menos saltos, facturación clara. Pero cambiar de modelo obliga a otro cliente,
otra clave y otro formato de respuesta.

**B. OpenAI directo.**
Igual, con el mismo acoplamiento.

**C. OpenRouter como gateway.**
Una API compatible con OpenAI que enruta a decenas de modelos.

## Decisión

**Opción C**, con `anthropic/claude-sonnet-4` como modelo principal
([`openai.go`](../../guardian-ai/backend/openai.go)) y OpenAI directo
con `gpt-4o` como fallback ([`openai.go`](../../guardian-ai/backend/openai.go)).

Los embeddings van por OpenAI directo: OpenRouter no los enruta.

## Consecuencias

**A favor**

- **Cambiar de modelo es una variable de entorno.** Durante el desarrollo se
  compararon modelos sin tocar código.
- Una sola clave y una sola factura para todos los proveedores de chat.
- Formato compatible con OpenAI: el parseo de tool calls es uno solo.
- **Un fallback que existe de verdad**, con proveedor distinto: si OpenRouter
  cae, OpenAI directo no cae con él.

**En contra**

- **Un intermediario más** entre el sistema y el modelo: un punto de fallo y
  latencia adicionales.
- Los precios de OpenRouter llevan margen sobre el proveedor.
- **Doble dependencia de claves**: chat por OpenRouter, embeddings por OpenAI.
  Falta cualquiera de las dos y algo deja de funcionar. `/api/capabilities` lo
  reporta, pero el acoplamiento existe.
- Un incidente en OpenRouter es un incidente propio, y su estado no se controla.

**Cuándo revisar**

Con volumen de producción, el margen deja de ser despreciable y conviene ir
directo al proveedor. Para entonces la elección de modelo ya estará tomada, que
era justo lo que el gateway servía para decidir.

## Ver también

- [LLM](../llm.md)
