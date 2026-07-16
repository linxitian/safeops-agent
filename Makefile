GO ?= go
NPM ?= npm
CGO_ENABLED ?= 0
GO_COMMANDS := $(notdir $(wildcard cmd/*))

.PHONY: test lint build build-native build-linux-amd64 build-loong64 frontend-test frontend-lint frontend-build target-test benchmark release clean

test: frontend-test
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test ./...

lint: frontend-lint
	CGO_ENABLED=$(CGO_ENABLED) $(GO) vet ./...

frontend-test:
	$(NPM) --prefix web test

frontend-lint:
	$(NPM) --prefix web run lint

frontend-build:
	$(NPM) --prefix web run build

build: build-linux-amd64 frontend-build

build-native:
	@mkdir -p bin
	@for command in $(GO_COMMANDS); do \
		CGO_ENABLED=$(CGO_ENABLED) $(GO) build -trimpath -o bin/$$command ./cmd/$$command || exit 1; \
	done

build-linux-amd64:
	@mkdir -p bin/linux-amd64
	@for command in $(GO_COMMANDS); do \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -o bin/linux-amd64/$$command ./cmd/$$command || exit 1; \
	done

build-loong64:
	@./scripts/check-cross-build.sh loong64

target-test:
	@./scripts/target-test.sh

benchmark:
	@CGO_ENABLED=$(CGO_ENABLED) $(GO) run ./cmd/safeops-bench all \
		--policy ./policies/tools.yaml \
		--output-dir ./artifacts/benchmark

release:
	@./scripts/build-release.sh

clean:
	rm -rf bin dist web/dist
