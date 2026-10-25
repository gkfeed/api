.PHONY: build check dev format install-lint lint merge-to-master swagger swagger-check test update vet

APP_DIR := app
GO_BIN := $(shell go env GOBIN)
ifeq ($(GO_BIN),)
GO_BIN := $(shell go env GOPATH)/bin
endif
GOPLS ?= $(GO_BIN)/gopls

update:
	git fetch && git pull
	docker compose stop && docker compose rm -f
	docker compose build
	docker compose up -d

swagger:
	./scripts/swagger.sh generate

swagger-check: swagger
	./scripts/swagger.sh validate

build: swagger
	mkdir -p .cache/bin
	cd $(APP_DIR) && go build -trimpath -buildvcs=false -o ../.cache/bin/gkfeed-api ./cmd/api

test: swagger
	cd $(APP_DIR) && go test ./...

vet: swagger
	cd $(APP_DIR) && go vet ./...

check: test vet swagger-check

lint: vet
	@test -x "$(GOPLS)" || { \
		echo "gopls is not installed; run 'make install-lint'" >&2; \
		exit 1; \
	}
	@cd $(APP_DIR) && { \
		diagnostics="$$( "$(GOPLS)" check $$(find . -type f -name '*.go' -print) 2>&1 )"; \
		status=$$?; \
		if test -n "$$diagnostics"; then printf '%s\n' "$$diagnostics"; fi; \
		test $$status -eq 0 && test -z "$$diagnostics"; \
	}

format: swagger
	cd $(APP_DIR) && go fmt ./...

install-lint:
	go install golang.org/x/tools/gopls@latest

dev: swagger
	cd $(APP_DIR) && go run ./cmd/api

merge-to-master:
	git checkout master
	git merge dev
	git push origin master
	git checkout dev
