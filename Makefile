GO ?= go
NODE ?= node

.PHONY: ci build test test-js check-js check-web web site lint vet vet-wasm wasm fmt check-fmt run clean

# ci must pass before any commit.
ci: check-fmt check-js check-web vet vet-wasm lint build wasm test test-js

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
	$(GO) run ./cmd/mvp

clean:
	$(GO) clean ./...
