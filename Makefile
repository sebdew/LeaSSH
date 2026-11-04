.PHONY: dev test lint build web docker
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

test:
	go test ./...
	cd web && npm test

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	cd web && npm run typecheck

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/leassh ./cmd/leassh

# Image multi-arch ; ajoutez --push (ou --load pour une seule plateforme).
docker:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t leassh:$(VERSION) .
