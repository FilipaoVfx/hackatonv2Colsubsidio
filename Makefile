# Secura — atajos de desarrollo.
# `make` sin argumentos lista los targets.

COMPOSE  := docker compose -f guardian-ai/docker-compose.yml
BACKEND  := guardian-ai/backend
CLI      := guardian-ai/cli
API      ?= http://localhost:8099

.DEFAULT_GOAL := help
.PHONY: help dev up down restart logs health test test-backend test-cli lint build build-cli docker clean e2e tunnels release

help: ## Muestra esta ayuda
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[33m%-14s\033[0m %s\n", $$1, $$2}'

## ---- Entorno ----

dev: ## Levanta todo el stack (build + up). Punto de partida
	@test -f guardian-ai/.env || { echo "Falta guardian-ai/.env — copia .env.example y rellena"; exit 1; }
	$(COMPOSE) up -d --build
	@echo "Listo en $(API) — verifica con: make health"

up: ## Levanta sin reconstruir
	$(COMPOSE) up -d

down: ## Detiene y elimina los contenedores
	$(COMPOSE) down

restart: ## Reinicia el backend (recarga corpus RAG y config)
	$(COMPOSE) restart backend

logs: ## Sigue los logs del backend
	$(COMPOSE) logs -f backend

health: ## Verifica salud y las 7 capabilities
	@curl -fsS $(API)/api/health | python3 -m json.tool
	@curl -fsS $(API)/api/capabilities | python3 -c \
		"import json,sys; d=json.load(sys.stdin); \
		[print(('  \033[32m✓\033[0m ' if v else '  \033[31m✗\033[0m ')+k) \
		for k,v in sorted(d.items()) if isinstance(v,bool)]"

## ---- Calidad ----

test: test-backend test-cli ## Corre todas las suites

test-backend: ## Tests del backend (22 suites, sin red ni claves)
	cd $(BACKEND) && go test ./...

test-cli: ## Tests de la CLI
	cd $(CLI) && go test ./...

lint: ## go vet en backend y CLI
	cd $(BACKEND) && go vet ./...
	cd $(CLI) && go vet ./...

## ---- Build ----

build: ## Compila el backend
	cd $(BACKEND) && go build -o bin/guardian .

build-cli: ## Compila la CLI
	cd $(CLI) && go build -o bin/secura .

docker: ## Reconstruye las imágenes sin caché
	$(COMPOSE) build --no-cache

release: ## Publica el release de Windows de la CLI
	bash $(CLI)/scripts/release.sh

## ---- Operación ----

e2e: ## Dispara una conversación real de WhatsApp (gasta tokens)
	curl -sX POST $(API)/api/whatsapp/simulate-inbound \
		-H 'Content-Type: application/json' \
		-d '{"from":"5730012345$(shell date +%2N)","text":"quiero asegurar mi carro"}'

tunnels: ## Rota los túneles Cloudflare y publica el endpoint
	bash $(CLI)/scripts/tunnels.sh

clean: ## Borra binarios y artefactos de build
	rm -rf $(BACKEND)/bin $(CLI)/bin $(CLI)/dist
	$(COMPOSE) down -v
