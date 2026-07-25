# 07 — Runbook: Exponer la Colsubsidio Protege API para consumo desde cualquier entorno

> **Guardian AI** · Runbook de operación
> Contexto: la API (`http://147.93.11.136:9000`, OpenAPI 0.1.0, VPS Hostinger `srv1369270.hstgr.cloud`) filtra por IP de origen; nuestro egress `137.184.139.130` está dropeado (ver `06_FEATURE_CHAT_WHATSAPP.md` §8). Mientras tanto la demo corre contra el mock local `mock-protege/` con el mismo contrato.
> Objetivo de este runbook: dejar la API consumible **desde cualquier entorno** de forma segura.

## Regla de oro

**No abrir el puerto 9000 directo a internet.** La API maneja datos personales (teléfonos, respuestas de perfil) y su spec **no declara autenticación**: exponerla en HTTP plano la deja abierta a cualquier escáner. La forma correcta es poner un proxy delante con **HTTPS + Bearer token**.

## Opción recomendada — Caddy con HTTPS + Bearer token (~30 min)

```bash
# 1) SSH al VPS (Hostinger: hPanel → VPS → Terminal del navegador, o directo)
ssh root@147.93.11.136

# 2) Verificar dónde escucha la app
ss -tlnp | grep 9000
#   Ideal: 127.0.0.1:9000 (solo localhost). Si dice 0.0.0.0:9000, se cierra en el paso 4.

# 3) DNS: crear un registro A, ej. api.tudominio.com -> 147.93.11.136
#    (sin dominio no hay HTTPS serio; cualquier subdominio sirve)

# 4) Abrir SOLO 80/443 (el 80 lo usa Caddy para el reto Let's Encrypt)
ufw allow 80/tcp && ufw allow 443/tcp
ufw deny 9000/tcp        # el proxy llega por localhost; el 9000 no necesita exposición
ufw enable

# 5) Instalar Caddy (Ubuntu)
apt install -y caddy

# 6) Configurar /etc/caddy/Caddyfile
cat > /etc/caddy/Caddyfile <<'EOF'
api.tudominio.com {
    # Auth por token (el spec de la API no declara auth -> se la ponemos aquí)
    @authorized header Authorization "Bearer PON_AQUI_UN_TOKEN_LARGO_ALEATORIO"
    handle @authorized {
        reverse_proxy 127.0.0.1:9000
    }
    handle {
        respond "unauthorized" 401
    }
    header {
        X-Content-Type-Options nosniff
        -Server
    }
    request_body { max_size 256KB }
}
EOF
systemctl reload caddy   # Caddy emite el certificado TLS automáticamente

# 7) Si el firewall de Hostinger (hPanel → VPS → Firewall) está activo:
#    ALLOW tcp 80, 443 desde 0.0.0.0/0  ·  DENY tcp 9000 desde 0.0.0.0/0

# 8) Verificación desde cualquier entorno
curl -H "Authorization: Bearer PON_AQUI_UN_TOKEN_LARGO_ALEATORIO" \
     https://api.tudominio.com/api/v1/products   # -> 200 JSON
curl https://api.tudominio.com/api/v1/products  # -> 401
```

### Cómo se conecta Guardian AI (cero cambios de código)

El cliente Go (`guardian-ai/backend/colsubsidio.go`) **ya envía `Authorization: Bearer` cuando `COLSUBSIDIO_API_TOKEN` está seteado**. En `.env`:

```bash
COLSUBSIDIO_API_URL=https://api.tudominio.com
COLSUBSIDIO_API_TOKEN=PON_AQUI_UN_TOKEN_LARGO_ALEATORIO
```

`docker compose up -d backend` y la demo pasa del mock a la API real.

## Opción rápida (solo si urge la hackathon — asumir el riesgo)

```bash
ufw allow 9000/tcp          # + app bindeando 0.0.0.0:9000
```

⚠️ Queda **sin auth y en HTTP plano**: cualquiera que escanee el puerto puede leer/escribir datos. Si se usa, mínimo:

- Restringir por IP de los entornos conocidos (`ufw allow from <ip> to any port 9000`).
- Retirar la regla después del evento (`ufw delete allow 9000/tcp`).

## Checklist para producción seria

- [ ] **Rate limiting** (plugin de Caddy o nginx `limit_req`) — una API sin auth es abusable por bots.
- [ ] **Monitoreo**: chequear `GET /api/v1/health` cada minuto (Uptime Kuma / cron con alerta).
- [ ] **fail2ban** sobre el 443 para ráfagas de 401.
- [ ] **Backups** de la data de conversaciones (volumen/DB de la API).
- [ ] **CORS** solo si el consumo será desde browser (server-to-server no lo necesita); orígenes exactos, nunca `*` con credenciales.
- [ ] Datos personales colombianos → considerar deberes de **Ley 1581 (habeas data)**: HTTPS en tránsito es lo mínimo.

## Verificación posterior (desde Guardian AI)

```bash
curl -s http://localhost:8099/api/capabilities   # "colsubsidio": true con la URL real
docker logs guardian-ai-backend-1 | grep protege  # "colsubsidio protege api enabled"
# Flujo completo: /chat → Iniciar contacto → preguntas reales de la API → recomendación → Pipeline
```
