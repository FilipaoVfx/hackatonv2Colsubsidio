#!/usr/bin/env bash
# Starts (or restarts) the two Cloudflare quick tunnels the jury depends on and
# writes their current hostnames to the landing repo's secura-endpoint.json.
#
#   ./tunnels.sh          start both, publish, commit and push
#   ./tunnels.sh --local  start both and publish locally, no push
#
# Quick tunnels get a fresh hostname on every launch. That is fine — no printed
# instruction, released binary or landing page ever names them. They are only
# ever read from the discovery JSON, so a rotation costs one run of this script.
set -euo pipefail

LOCAL_ONLY=0
[[ "${1:-}" == "--local" ]] && LOCAL_ONLY=1

API_PORT=8099
TTYD_PORT=7682
LANDING=/root/landing-deploy
LOG_DIR=/root/.secura
mkdir -p "$LOG_DIR"

start_tunnel() {
  local name="$1" port="$2" log="$LOG_DIR/$1.log"
  : > "$log"
  pm2 delete "$name" >/dev/null 2>&1 || true
  pm2 start cloudflared --name "$name" -- \
    tunnel --url "http://localhost:${port}" --no-autoupdate \
    --logfile "$log" >/dev/null

  # cloudflared prints the assigned hostname once, a second or two after start.
  local url=""
  for _ in $(seq 1 30); do
    sleep 1
    url=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$log" | head -1 || true)
    [[ -n "$url" ]] && break
  done
  [[ -z "$url" ]] && { echo "no se pudo obtener la URL de $name" >&2; exit 1; }
  echo "$url"
}

echo "==> tunnel API (:$API_PORT)"
API_URL=$(start_tunnel secura-api-tunnel "$API_PORT")
echo "    $API_URL"

echo "==> tunnel terminal web (:$TTYD_PORT)"
WEB_URL=$(start_tunnel secura-ttyd-tunnel "$TTYD_PORT")
echo "    $WEB_URL"

echo "==> verificando"
curl -fsS "$API_URL/api/health" >/dev/null && echo "    API health OK"
curl -fsS -o /dev/null "$WEB_URL/" && echo "    terminal web OK"

cat > "$LANDING/public/secura-endpoint.json" <<EOF
{
  "api_url": "$API_URL",
  "web_terminal": "$WEB_URL",
  "updated": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
echo "==> escrito $LANDING/public/secura-endpoint.json"

if [[ $LOCAL_ONLY -eq 1 ]]; then
  echo "==> --local: no se publica"
  exit 0
fi

cd "$LANDING"
if [[ -d .git ]]; then
  git add public/secura-endpoint.json
  git commit -q -m "chore: rotar endpoints de secura" || true
  git push -q origin HEAD
  echo "==> publicado — Cloudflare Pages redespliega en ~40s"
else
  echo "==> $LANDING no es un repo git; copia el JSON al repo del landing a mano" >&2
fi
