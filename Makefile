.PHONY: swagger openapi
swagger:
	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/server/main.go

openapi:
	@echo "OpenAPI spec available at: http://localhost:8080/openapi.yaml"
	@echo "View with Swagger Editor: https://editor.swagger.io/"

.PHONY: run
run:
	set -a && source .env && set +a && go run cmd/server/main.go

.PHONY: build
build:
	go build -o bin/server cmd/server/main.go

.PHONY: test
test:
	./run_tests.sh

.PHONY: test-services
test-services:
	go test -v -race ./internal/services/...

.PHONY: test-coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./internal/services/...
	go tool cover -html=coverage.out -o coverage.html

.PHONY: test-clean
test-clean:
	rm -f coverage.out coverage.html