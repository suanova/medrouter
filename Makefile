GO ?= go
BUN ?= bun
BIN ?= bin/medrouter
DSH_IMAGE ?= harbor.isuanova.com/suanova/medrouter-dsh:latest

.PHONY: help run build build-web dev-web build-dsh-image test test-go test-web test-dsh-plugins check vet typecheck fmt clean

help:
	@echo "MedRouter"
	@echo ""
	@echo "  make build       Build the frontend, then the Go binary (bin/medrouter)"
	@echo "  make run         Run the Go server (requires web/dist or make build-web)"
	@echo "  make dev-web     Start the Rsbuild dev server on :5173"
	@echo "  make build-dsh-image  Build the DSH runtime image (images/dsh)"
	@echo "  make test        Run Go and frontend tests"
	@echo "  make test-dsh-plugins  Test the bundled dsh plugins (node --test)"
	@echo "  make check       go vet + tsc + tests"
	@echo "  make fmt         gofmt all Go files"
	@echo "  make clean       Remove build output"

run:
	@test -f web/dist/index.html || { echo "web/dist is missing; run 'make build-web' first"; exit 1; }
	$(GO) run .

build: build-web
	@mkdir -p $(dir $(BIN))
	$(GO) build -trimpath -o $(BIN) .

build-web:
	cd web && $(BUN) install --frozen-lockfile && $(BUN) run build

dev-web:
	cd web && $(BUN) run dev

build-dsh-image:
	docker build -t $(DSH_IMAGE) images/dsh

test: test-go test-web

test-dsh-plugins:
	node --test images/dsh/plugins/dsh-api/test/isolation.test.mjs
	node --test images/dsh/plugins/medrouter-skills/test/skills.test.mjs

test-go:
	$(GO) test ./... -count=1

test-web:
	cd web && $(BUN) run test

check: vet typecheck test

vet:
	$(GO) vet ./...

typecheck:
	cd web && $(BUN) run typecheck

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './web/*')

clean:
	rm -rf bin web/dist
