.PHONY: all api ui dev dev-ai ai-up ai-down install build test test-e2e test-e2e-install test-all clean help \
	docker-build docker-build-multiarch docker-up docker-down \
	docker-build-api docker-push-api docker-push-api-multiarch push-api \
	docker-build-web docker-push-web docker-push-web-multiarch push-web \
	docker-build-all docker-push-all docker-push-all-multiarch push-all

# Docker image repository and tag configuration (can be overridden via CLI, e.g. make push-all REGISTRY=... TAG=...)
REGISTRY ?= harbor.coffee-iot.com:8888/vvvv
TAG ?= latest
PLATFORMS ?= linux/amd64,linux/arm64

API_IMAGE ?= $(REGISTRY)/openchore-api:$(TAG)
WEB_IMAGE ?= $(REGISTRY)/openchore-web:$(TAG)

# Default target
all: help

# Run the API server
api:
	go run cmd/server/main.go

# Run the UI (Vite)
ui:
	cd web && npm run dev

# Run both API and UI concurrently (fresh DB each time)
# The server auto-seeds from config/config.yaml on first boot when the DB is empty.
dev:
	@test -f config/config.yaml || (cp config/config.example.yaml config/config.yaml && echo "Created config/config.yaml from example")
	rm -f openchore.db openchore.db-shm openchore.db-wal
	make -j 2 api ui

# Run both API and UI with the local AI services (llama.cpp + Kokoro)
dev-ai: ai-up
	@test -f config/config.yaml || (cp config/config.example.yaml config/config.yaml && echo "Created config/config.yaml from example")
	rm -f openchore.db openchore.db-shm openchore.db-wal
	AI_BASE_URL=http://localhost:8081/v1 AI_MODEL=gemma-4-e4b TTS_BASE_URL=http://localhost:8880/v1 make -j 2 api ui

# Start the local AI services with ports published for local dev
ai-up:
	docker compose -f compose.yaml -f compose.dev-ai.yaml --profile ai --profile tts up -d llama kokoro

# Stop the local AI services
ai-down:
	docker compose -f compose.yaml -f compose.dev-ai.yaml --profile ai --profile tts down

# Install dependencies for both
install:
	go mod download
	cd web && npm install

# Build both API and UI
build:
	go build -o server cmd/server/main.go
	cd web && npm run build

# Run Go tests
test:
	go test ./...

# Install e2e test dependencies
test-e2e-install:
	cd e2e && npm install && npx playwright install --with-deps chromium

# Run e2e tests (starts servers automatically, fresh DB)
test-e2e:
	@test -f config/config.yaml || (cp config/config.example.yaml config/config.yaml && echo "Created config/config.yaml from example")
	cd e2e && npx playwright test

# Run all tests (Go unit + e2e)
test-all: test test-e2e

# Clean up build artifacts and database
clean:
	rm -f server openchore.db openchore.db-shm openchore.db-wal

# Docker targets (supports both Mac ARM64 and Linux AMD64)
docker-build:
	docker compose build

docker-build-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -t openchore-api:latest .
	docker buildx build --platform linux/amd64,linux/arm64 -t openchore-web:latest ./web

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

# API image targets
docker-build-api:
	docker build -t $(API_IMAGE) .

docker-push-api: docker-build-api
	docker push $(API_IMAGE)

docker-push-api-multiarch:
	docker buildx build --platform $(PLATFORMS) -t $(API_IMAGE) --push .

push-api: docker-push-api

# Web image targets
docker-build-web:
	docker build -t $(WEB_IMAGE) ./web

docker-push-web: docker-build-web
	docker push $(WEB_IMAGE)

docker-push-web-multiarch:
	docker buildx build --platform $(PLATFORMS) -t $(WEB_IMAGE) --push ./web

push-web: docker-push-web

# Combined image targets
docker-build-all: docker-build-api docker-build-web

docker-push-all: docker-push-api docker-push-web

docker-push-all-multiarch: docker-push-api-multiarch docker-push-web-multiarch

push-all: docker-push-all

# Show help
help:
	@echo "Available targets:"
	@echo "  api     - Run the API server (Go)"
	@echo "  ui      - Run the UI (Vite)"
	@echo "  dev     - Run both API and UI concurrently (fresh DB, auto-seeded from config)"
	@echo "  dev-ai  - Same as dev but with local AI (llama.cpp + Kokoro in Docker)"
	@echo "  ai-up   - Start the local AI services in Docker"
	@echo "  ai-down - Stop the local AI services"
	@echo "  install - Install dependencies for both API and UI"
	@echo "  build   - Build both API and UI"
	@echo ""
	@echo "Docker Build & Push targets:"
	@echo "  docker-build              - Build Docker compose images locally"
	@echo "  docker-up / docker-down   - Start / Stop Docker Compose services"
	@echo "  push-api                  - Build and push API image ($(API_IMAGE))"
	@echo "  push-web                  - Build and push Web image ($(WEB_IMAGE))"
	@echo "  push-all                  - Build and push both API and Web images"
	@echo "  docker-push-api-multiarch - Build & push multi-arch API ($(PLATFORMS))"
	@echo "  docker-push-web-multiarch - Build & push multi-arch Web ($(PLATFORMS))"
	@echo "  docker-push-all-multiarch - Build & push multi-arch both API and Web"
	@echo ""
	@echo "  Override registry/tag example:"
	@echo "    make push-all REGISTRY=your-registry.com/repo TAG=v1.0.0"
	@echo ""
	@echo "Test & Clean targets:"
	@echo "  test              - Run Go tests"
	@echo "  test-e2e-install  - Install e2e test dependencies (Playwright + Chromium)"
	@echo "  test-e2e          - Run e2e tests (starts servers automatically)"
	@echo "  test-all          - Run Go tests + e2e tests"
	@echo "  clean             - Clean up build artifacts and database"

