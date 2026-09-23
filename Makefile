# Repository tasks. The module list comes from go.work, so a new module joins
# every target by itself.
#
# The tools commands under tools/ carry the checks that need more than a shell
# loop: the workspace reader, the require and tidy checks, and the Go floor.
MODULES := $(shell go work edit -json | grep '"DiskPath"' | sed -E 's/.*"DiskPath": "(.*)"/\1/')
FUZZTIME ?= 30s

.PHONY: test race fuzz bench lint ste snippets verifyplan tidy tidy-check requires floor release-check compat cover vuln map integration

test:
	@for m in $(MODULES); do (cd $$m && go test ./...) || exit 1; done

race:
	@for m in $(MODULES); do (cd $$m && go test -race ./...) || exit 1; done

# fuzz runs every Fuzz target of every module, and finds them itself.
fuzz:
	go run ./tools/cmd/fuzz -time $(FUZZTIME)

# bench compares the benchmarks with bench/baseline.txt. A benchmark that is
# slower by more than 20% fails.
bench:
	go run ./tools/cmd/bench -max-slowdown 20

# vuln scans an upgraded copy of each module's build list, which is what a user
# gets from a newer library.
vuln:
	go run ./tools/cmd/vuln

# cover fails a root package that falls under 85% without a line in
# tools/cover-known-low.txt.
cover:
	go run ./tools/cmd/cover -min 85

lint:
	@for m in $(MODULES); do (cd $$m && go vet ./... && golangci-lint run ./...) || exit 1; done

# tidy repairs the module files. tidy-check only reports, which is what CI runs.
tidy:
	go run ./tools/cmd/tidy -check=false
	go work sync

tidy-check:
	go run ./tools/cmd/tidy -check

# ste runs the Simple English lint over the markdown documents.
ste:
	go run ./tools/cmd/ste

# docs builds the two language-model documents from the repository pages.
docs:
	go run ./tools/cmd/docs

# snippets compiles every fenced Go block in the documentation, and runs the
# blocks that ask to run.
snippets:
	go run ./tools/cmd/snippets

# verifyplan runs the Verify command of every task in tasks/plan.md. It checks
# one task at a time with ONLY=<task-id>.
verifyplan:
	go run ./tools/cmd/verifyplan -only "$(ONLY)"

# requires checks the sibling requires, the release version, and the local replace.
requires:
	go run ./tools/cmd/requires

# floor tests every module with GOWORK=off on the oldest Go it claims, and with
# -libs also against an upgraded dependency set.
floor:
	go run ./tools/cmd/floor -libs

# release-check prints the plan of the next release: the tag order, the require
# updates, and the API difference against the last tag.
release-check:
	go run ./tools/cmd/release -version $(VERSION)

# compat runs the root module on an older toolchain than the one that builds it.
compat:
	GOWORK=off GOTOOLCHAIN=go1.23.0 go test ./...

# map scores the example tree against the wlog map rules.
map:
	go run github.com/jeremygprawira/wlog/cmd/wlog map --min-score 80 ./examples/...

# Optional: needs a running docker daemon. Not part of make test or make race.
integration:
	@mkdir -p .integration-out
	docker compose -f docker-compose.integration.yml up -d --wait
	@CLICKHOUSE_USER=wlog CLICKHOUSE_PASSWORD=wlog go test -tags=integration -timeout 600s ./drain/loki ./drain/otlp ./drain/clickhouse ./drain/elastic ./drain/splunk ./drain/victorialogs ./preset; status=$$?; \
		if [ $$status -ne 0 ]; then docker compose -f docker-compose.integration.yml ps -a; \
			docker compose -f docker-compose.integration.yml logs --no-color --tail 50; fi; \
		docker compose -f docker-compose.integration.yml down; exit $$status
