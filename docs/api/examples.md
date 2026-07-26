# Ejemplos de la API

Todos ejecutables contra un backend local (`make dev`). Pegan y funcionan.

```bash
API=http://localhost:8099
```

## Comprobación rápida

```bash
curl -s $API/api/health | jq
curl -s $API/api/capabilities | jq 'to_entries|map(select(.value|type=="boolean"))|from_entries'
```

Equivalente con la CLI, más legible:

```bash
secura doctor
```

## Disparar una conversación real

```bash
# Número fresco: reusar uno continúa la conversación anterior
PHONE="57300$(date +%7N)"

curl -sX POST $API/api/whatsapp/simulate-inbound \
  -H 'Content-Type: application/json' \
  -d "{\"from\":\"$PHONE\",\"text\":\"quiero asegurar mi carro\"}"
```

Míralo en vivo en otra terminal, antes de disparar:

```bash
secura tail
```

## Seguir una conversación

```bash
CALL=$(curl -s $API/api/calls | jq -r '.[0]')

# El log completo — la fuente de verdad
curl -s $API/api/calls/$CALL/events | jq -r \
  '.[] | "\(.sequence)\t\(.type)"'

# Solo lo que costó
curl -s $API/api/calls/$CALL/events | jq -r \
  '.[] | select(.type=="LLM_RESPONSE") |
   "\(.payload.latency_ms)ms  \(.payload.tokens_in)/\(.payload.tokens_out) tok  $\(.payload.cost_usd)"'

# Qué recuperó el RAG y con cuánta confianza
curl -s $API/api/calls/$CALL/events | jq -r \
  '.[] | select(.type=="KNOWLEDGE_RETRIEVED") | .payload'

# Qué herramientas ejecutó
curl -s $API/api/calls/$CALL/events | jq -r \
  '.[] | select(.type=="TOOL_EXECUTED") | "\(.payload.tool) — \(.payload.latency_ms)ms"'
```

## KPIs

```bash
curl -s $API/api/analytics/kpis | jq

# Costo por conversación
curl -s $API/api/analytics/kpis | jq \
  '{costo: .cost_usd, llamadas: .leads_whatsapp,
    por_llamada: (.cost_usd / .leads_whatsapp)}'
```

## Agent Studio

```bash
# Estado actual
curl -s $API/api/studio/config | jq '.published.persona'
curl -s $API/api/studio/versions | jq -r \
  '.versions[] | "v\(.version)\t\(.status)\t\(.note)"'

# Borrador — NO afecta producción
curl -sX PUT $API/api/studio/config/draft \
  -H 'Content-Type: application/json' \
  -d '{"persona":{"formality":8,"emojis":1}}'
```

> ⚠️ `publish` y `rollback` **escriben en el agente que atiende clientes
> reales**. No los pegues sin querer.

```bash
# curl -sX POST $API/api/studio/config/publish
# curl -sX POST $API/api/studio/config/rollback/12
```

## WebSocket

```bash
# Verificar el upgrade (--http1.1 es obligatorio: HTTP/2 no soporta este handshake)
curl -sI --http1.1 \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  $API/ws | head -1
# → HTTP/1.1 101 Switching Protocols

# Consumir el stream
websocat ws://localhost:8099/ws | jq -r '"\(.type)\t\(.call_id[0:8])"'
```

## Postman

No hay colección: los endpoints están en [README](README.md) y estos ejemplos
cubren los flujos reales. Para importar, `curl` se convierte directo desde
Postman con *Import → Raw text*.
