GO ?= go
NODE ?= node

.PHONY: ci build test test-js check-js lint vet fmt check-fmt run clean

# ci must pass before any commit.
ci: check-fmt check-js vet lint build test test-js

build:
	$(GO) build ./...

test:
	$(GO) test ./...

test-js:
	$(NODE) --test web/funroute-core.test.mjs

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

run:
	$(GO) run ./cmd/mvp

clean:
	$(GO) clean ./...
