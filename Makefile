MODULES := $(shell go work edit -json | grep '"DiskPath"' | sed -E 's/.*"DiskPath": "(.*)"/\1/')

.PHONY: test race fuzz bench lint tidy cover compat map

test:
	@for m in $(MODULES); do (cd $$m && go test ./...) || exit 1; done

race:
	@for m in $(MODULES); do (cd $$m && go test -race ./...) || exit 1; done

fuzz:
	go test -run=xxx -fuzz=FuzzRedact_NeverLeaks -fuzztime=30s ./redact

bench:
	@for m in $(MODULES); do (cd $$m && go test -run=xxx -bench=. -benchmem ./...) || exit 1; done

lint:
	@for m in $(MODULES); do (cd $$m && go vet ./... && golangci-lint run ./...) || exit 1; done

tidy:
	@for m in $(MODULES); do (cd $$m && go mod tidy) || exit 1; done
	go work sync

cover:
	mkdir -p coverage
	go test -coverprofile=coverage/coverage.out ./...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html

compat:
	GOWORK=off GOTOOLCHAIN=go1.23.0 go test ./...

map:
	go run ./cmd/wlog map --min-score 80 ./examples/...
