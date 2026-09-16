# Repository tasks. The module list comes from go.work, so a new module joins
# every target by itself.
#
# The tools commands under tools/ carry the checks that need more than a shell
# loop: the workspace reader, the require and tidy checks, and the Go floor.
MODULES := $(shell go work edit -json | grep '"DiskPath"' | sed -E 's/.*"DiskPath": "(.*)"/\1/')
FUZZTIME ?= 30s

.PHONY: test race fuzz bench lint tidy tidy-check requires floor compat cover map integration

test:
	@for m in $(MODULES); do (cd $$m && go test ./...) || exit 1; done

race:
	@for m in $(MODULES); do (cd $$m && go test -race ./...) || exit 1; done

fuzz:
	go test -run=xxx -fuzz=FuzzRedact_NeverLeaks -fuzztime=$(FUZZTIME) ./redact

bench:
	@for m in $(MODULES); do (cd $$m && go test -run=xxx -bench=. -benchmem ./...) || exit 1; done

lint:
	@for m in $(MODULES); do (cd $$m && go vet ./... && golangci-lint run ./...) || exit 1; done

# tidy repairs the module files. tidy-check only reports, which is what CI runs.
tidy:
	go run ./tools/cmd/tidy -check=false
	go work sync

tidy-check:
	go run ./tools/cmd/tidy -check

# requires checks the sibling requires, the release version, and the local replace.
requires:
	go run ./tools/cmd/requires

# floor tests every module with GOWORK=off on the oldest Go it claims, and with
# -libs also against an upgraded dependency set.
floor:
	go run ./tools/cmd/floor -libs

# compat runs the root module on an older toolchain than the one that builds it.
compat:
	GOWORK=off GOTOOLCHAIN=go1.23.0 go test ./...

cover:
	mkdir -p coverage
	go test -coverprofile=coverage/coverage.out ./...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html

# map scores the example tree against the wlog map rules.
map:
	go run github.com/jeremygprawira/wlog/cmd/wlog map --min-score 80 ./examples/...

# Optional: needs a running docker daemon. Not part of make test or make race.
integration:
	@mkdir -p .integration-out
	docker compose -f docker-compose.integration.yml up -d
	@go test -tags=integration -timeout 180s ./drain/loki ./drain/otlp ./drain/clickhouse; status=$$?; \
		docker compose -f docker-compose.integration.yml down; exit $$status
