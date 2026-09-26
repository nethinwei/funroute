GO ?= go
NODE ?= node

.PHONY: ci build test test-js check-js check-web web site lint vet vet-wasm wasm fmt check-fmt check-imports staticcheck modernize golangci deadcode run clean limits golden perf

# ci must pass before any commit.
ci: check-fmt check-imports check-js check-web vet vet-wasm staticcheck modernize golangci deadcode lint build wasm test test-js

# The linters are tools, not dependencies: go.mod stays empty. Each is
# installed once, at the pinned version, into .tools/<version>/ — built for
# this machine, so it can then analyse the js/wasm entry with GOOS set — and a
# new version pin installs afresh rather than reusing an old binary.
STATICCHECK_VERSION := v0.8.1
X_TOOLS_VERSION     := v0.50.0
GOLANGCI_VERSION    := v2.11.4
TOOLS       := $(CURDIR)/.tools
STATICCHECK := $(TOOLS)/staticcheck-$(STATICCHECK_VERSION)/staticcheck
MODERNIZE   := $(TOOLS)/x-tools-$(X_TOOLS_VERSION)/modernize
GOIMPORTS   := $(TOOLS)/x-tools-$(X_TOOLS_VERSION)/goimports
DEADCODE    := $(TOOLS)/x-tools-$(X_TOOLS_VERSION)/deadcode
GOLANGCI    := $(TOOLS)/golangci-lint-$(GOLANGCI_VERSION)/golangci-lint

$(STATICCHECK):
	GOBIN=$(dir $@) $(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

$(MODERNIZE):
	GOBIN=$(dir $@) $(GO) install golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@$(X_TOOLS_VERSION)

$(GOIMPORTS):
	GOBIN=$(dir $@) $(GO) install golang.org/x/tools/cmd/goimports@$(X_TOOLS_VERSION)

$(DEADCODE):
	GOBIN=$(dir $@) $(GO) install golang.org/x/tools/cmd/deadcode@$(X_TOOLS_VERSION)

$(GOLANGCI):
	GOBIN=$(dir $@) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

# staticcheck.conf enables every check. The browser entry builds only for
# js/wasm, so it is checked there, as vet-wasm does.
staticcheck: $(STATICCHECK)
	$(STATICCHECK) ./...
	GOOS=js GOARCH=wasm $(STATICCHECK) ./web/wasm

# modernize reports code the current Go has a plainer spelling for.
modernize: $(MODERNIZE)
	$(MODERNIZE) ./...
	GOOS=js GOARCH=wasm $(MODERNIZE) ./web/wasm

# golangci-lint runs the linters .golangci.yml enables, on both builds.
golangci: $(GOLANGCI)
	$(GOLANGCI) run ./...
	GOOS=js GOARCH=wasm $(GOLANGCI) run ./web/wasm/...

# deadcode reports a function nothing reaches, not even a test: code to delete,
# or public API with no test. Its output is the failure.
deadcode: $(DEADCODE)
	@unreachable=$$($(DEADCODE) -test ./...) || exit 1; \
	if [ -n "$$unreachable" ]; then echo "$$unreachable"; exit 1; fi

# Imports come in two groups: the standard library, then this module.
check-imports: $(GOIMPORTS)
	@unsorted=$$($(GOIMPORTS) -local github.com/nethinwei/funroute -l . | grep -v node_modules); \
	if [ -n "$$unsorted" ]; then \
		echo "goimports -local github.com/nethinwei/funroute required for:"; echo "$$unsorted"; exit 1; \
	fi

# The front end is TypeScript bundled by esbuild into web/dist: one module per
# component (app, lsp, editor, contract, runner, canvas) and the chunks they
# share, so a page loads only what it uses. The bundle is built, never
# committed: the Go side does not need it, and whatever serves the workbench
# builds it first. check-web type-checks the sources and makes sure they build.
web:
	cd web && npm run -s build

check-web:
	cd web && npm run -s check
	$(MAKE) -s web

# The language server for the browser, and the Go runtime glue that loads it.
# wasm_exec.js has to come from the Go that built the module, so it is copied
# from GOROOT rather than kept in the repository.
wasm:
	@mkdir -p web/dist
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags="-s -w" -o web/dist/funroute.wasm ./web/wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" web/dist/wasm_exec.js

# The browser entry builds only for js/wasm, so the ordinary vet skips it.
vet-wasm:
	GOOS=js GOARCH=wasm $(GO) vet ./web/wasm

build:
	$(GO) build ./...

test:
	$(GO) test ./...

test-js:
	$(NODE) --test web/src/*.test.ts web/funroute-lsp.test.mjs

# limits writes the tables of docs/limits.md anew from a run; make test holds
# the file to them. perf measures the speed into docs/perf.md, committed when
# it is wanted: timings are the machine's, so nothing asserts them. The
# comparison with expr is a module of its own, tests/perf/expr, so that expr
# never enters go.mod; its section goes at the end of the report.
limits:
	$(GO) test ./tests/limits -update

# golden writes the record of every answer anew: only for a change meant to
# change one, whose diff is then read line by line.
golden:
	$(GO) test ./tests/golden -update

perf: wasm
	$(GO) run ./tests/perf > docs/perf.md
	cd tests/perf/expr && $(GO) run . >> ../../../docs/perf.md

check-js:
	@for file in web/*.js; do $(NODE) --check $$file || exit 1; done

# lint enforces the style budget: <=50 lines per func, <=3 nesting levels,
# <=800 lines per file. See tools/lint.
lint:
	$(GO) run ./tools/lint .

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

check-fmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required for:"; echo "$$unformatted"; exit 1; \
	fi

# site is the workbench as it is published: the page, its styles and data, the
# bundle and the language server, and nothing else of web/. make run serves
# it and the Pages workflow uploads it, so what works locally is what ships.
SITE_FILES = index.html favicon.svg tokens.css styles.css funroute-examples.json funroute-lsp-worker.js
site: web wasm
	rm -rf site && mkdir -p site/dist
	cp $(addprefix web/,$(SITE_FILES)) site/
	cp web/dist/*.js web/dist/funroute.wasm site/dist/

run: site
	$(GO) run ./cmd/playground

clean:
	$(GO) clean ./...
