BINARY := tally
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.buildVersion=$(VERSION)

.PHONY: build test lint fmt vet install clean run tidy

build: ## Build the tally binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test: ## Run the test suite
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## Format the code
	gofmt -w .

lint: vet ## Static checks (vet; add golangci-lint locally if available)
	@gofmt -l . | grep . && (echo "gofmt needed on the above files" && exit 1) || echo "gofmt clean"

install: ## go install into GOBIN
	go install -ldflags "$(LDFLAGS)" .

tidy: ## Tidy module deps
	go mod tidy

clean: ## Remove build artifacts
	rm -rf $(BINARY) dist/

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-10s %s\n", $$1, $$2}'
