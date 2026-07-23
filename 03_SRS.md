# Software Requirements Specification

## Funcionales
- Iniciar llamada.
- Mantener contexto.
- Transcribir audio.
- Actualizar Feature Store.
- Seleccionar narrativa.
- Recomendar seguro.
- Explicar recomendación.
- Mostrar Mission Control.
- Persistir eventos.
- Generar resumen.

## No funcionales
- Respuesta <2s.
- Dashboard en tiempo real.
- Arquitectura modular.
- Event Driven.
- Observabilidad.
- Adaptadores para proveedores.

## Eventos
CALL_STARTED
CALL_CONNECTED
USER_SPOKE
FEATURE_UPDATED
PROMPT_GENERATED
LLM_RESPONSE
TOOL_CALLED
CALL_ENDED

## Criterios de aceptación
El usuario completa una llamada, recibe una recomendación explicable y el dashboard refleja todo el pipeline en tiempo real.
