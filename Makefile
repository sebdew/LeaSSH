.PHONY: dev test lint build web docker e2e
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

# Backend + Vite (proxy API vers :8080). Clé maître de développement générée une fois dans .dev/.
dev:
	@mkdir -p .dev/data && test -f .dev/master.key || head -c32 /dev/urandom | base64 > .dev/master.key
	@echo "Interface : http://localhost:5173  (API : http://localhost:8080)"
	@(cd web && npm run dev) & \
	LEASSH_MASTER_KEY="$$(cat .dev/master.key)" LEASSH_DATA_DIR=.dev/data LEASSH_LISTEN=127.0.0.1:8080 go run ./cmd/leassh serve; \
	kill %1 2>/dev/null || true

web:
	cd web && npm ci --no-audit --no-fund && npm run build
	@touch web/dist/.gitkeep # vite vide dist/ ; le fichier doit rester versionné pour go:embed

test:
	go test ./...
	cd web && npm test

GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	go run $(GOLANGCI_LINT) run ./...
	cd web && npm run typecheck && npm run lint

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/leassh ./cmd/leassh

# Image multi-arch ; ajoutez --push (ou --load pour une seule plateforme).
docker:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t leassh:$(VERSION) .

# Parcours de bout en bout (Playwright, Chromium) sur le vrai binaire et une base neuve.
e2e: build
	cd web && npx playwright install --with-deps chromium && npm run e2e
