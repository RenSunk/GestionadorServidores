BINARY  := gestionador
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help dev-api dev-web web build build-all test lint clean

help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

dev-api: ## Backend Go en :8080 (logs en modo debug)
	go run ./cmd/gestionador -log-level debug

dev-web: ## Frontend Vite en :5173 (proxy de /api hacia :8080)
	cd web && npm run dev

web: ## Compila el frontend en web/dist
	cd web && npm ci && npm run build

build: web ## Binario único con el frontend embebido → bin/gestionador
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/gestionador

build-all: web ## Binarios para Linux amd64 y arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/gestionador
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/gestionador

test: ## Pruebas de Go
	go test ./...

lint: ## go vet + oxlint
	go vet ./...
	cd web && npm run lint

clean: ## Borra binarios y build del frontend
	rm -rf bin web/dist/assets web/dist/*.html web/dist/*.svg
