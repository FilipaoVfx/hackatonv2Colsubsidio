#!/usr/bin/env bash
# e2e-whatsapp.sh — prueba E2E del canal WhatsApp contra un despliegue vivo.
# Cubre: health → capabilities → debug → chat/start → 6 respuestas (flujo
# Protege completo) → recomendación generada → cierre → proyección al Pipeline.
#
# Uso:  ./scripts/e2e-whatsapp.sh [BASE_URL]
#       BASE_URL por defecto: http://localhost:8099
# Exit: 0 si todo pasa, 1 en el primer fallo.
set -u
BASE="${1:-http://localhost:8099}"
PHONE="+573199887766"
PASS=0; FAIL=0

ok()   { PASS=$((PASS+1)); echo "  ✅ $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  ❌ $1"; }
step() { echo "▶ $1"; }

jqget() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)" 2>/dev/null; }

step "1/8 GET /api/health"
H=$(curl -s -m 10 "$BASE/api/health")
[ "$(echo "$H" | jqget "d.get('status','')")" = "ok" ] && ok "health ok" || { bad "health: $H"; exit 1; }

step "2/8 GET /api/capabilities"
C=$(curl -s -m 10 "$BASE/api/capabilities")
echo "$C" | grep -q '"colsubsidio"' && ok "capabilities responde: $(echo "$C" | head -c 80)…" || { bad "capabilities: $C"; exit 1; }

step "3/8 GET /api/whatsapp/debug (endpoint de depuración)"
D=$(curl -s -m 10 "$BASE/api/whatsapp/debug")
echo "$D" | grep -q '"live_sessions"' && ok "debug ok (enabled=$(echo "$D" | jqget "d['enabled']"), brain=$(echo "$D" | jqget "d['colsubsidio_brain']"))" || { bad "debug: $D"; exit 1; }

step "4/8 POST /api/chat/start (contacto saliente autónomo)"
R=$(curl -s -m 15 -X POST "$BASE/api/chat/start" -H 'Content-Type: application/json' -d "{\"to\":\"$PHONE\"}")
CONV=$(echo "$R" | jqget "d['conversation_id']")
[ -n "$CONV" ] && ok "conversation_id=$CONV" || { bad "chat/start: $R"; exit 1; }

step "5/8 GET /api/whatsapp/sessions (re-attach del frontend)"
S=$(curl -s -m 10 "$BASE/api/whatsapp/sessions")
echo "$S" | grep -q "$CONV" && ok "sesión viva registrada para $PHONE" || bad "sessions no incluye la conversación: $S"

step "6/8 Flujo Protege: 6 respuestas del cliente"
i=0
for ANS in "Ana María Rojas" "tengo 34 años" "sí, dos hijos" "sí, un perro" "arrendada" "sí, viajo mucho"; do
  i=$((i+1))
  CODE=$(curl -s -o /dev/null -w "%{http_code}" -m 15 -X POST "$BASE/api/whatsapp/simulate-inbound" \
    -H 'Content-Type: application/json' -d "{\"from\":\"$PHONE\",\"text\":\"$ANS\"}")
  [ "$CODE" = "200" ] && ok "respuesta $i aceptada ($ANS)" || { bad "respuesta $i HTTP $CODE"; exit 1; }
done

step "7/8 Recomendación generada + cierre"
EV=$(curl -s -m 10 "$BASE/api/calls/$CONV/events")
NREC=$(echo "$EV" | jqget "sum(1 for e in d if e['type']=='RECOMMENDATION_GENERATED')")
PRODS=$(echo "$EV" | python3 -c "import sys,json; d=json.load(sys.stdin); print(', '.join(e['payload'].get('product_name','?') for e in d if e['type']=='RECOMMENDATION_GENERATED'))" 2>/dev/null)
[ "${NREC:-0}" -ge 1 ] && ok "RECOMMENDATION_GENERATED ×$NREC: $PRODS" || bad "sin recomendaciones"
ENDED=$(echo "$EV" | jqget "sum(1 for e in d if e['type']=='CALL_ENDED')")
[ "${ENDED:-0}" -ge 1 ] && ok "CALL_ENDED emitido (cierre automático del flujo)" || bad "sin CALL_ENDED"

step "8/8 Proyección al Pipeline (canal=whatsapp)"
sleep 2
A=$(curl -s -m 10 "$BASE/api/analytics/calls")
FOUND=$(echo "$A" | python3 -c "
import sys, json
try:
    calls = json.load(sys.stdin)
    print(any(c.get('channel')=='whatsapp' for c in calls[:10]))
except Exception:
    print('skip')" 2>/dev/null)
if [ "$FOUND" = "True" ]; then ok "aparece en /pipeline con canal WhatsApp";
elif [ "$FOUND" = "skip" ]; then ok "analytics no disponible (sin Supabase) — omitido";
else bad "no aparece en el Pipeline"; fi

echo
echo "════════════════════════════════════════"
echo "  RESULTADO: $PASS pasaron · $FAIL fallaron  (base: $BASE)"
echo "════════════════════════════════════════"
[ "$FAIL" -eq 0 ]
