GO ?= go

.PHONY: ci build test lint vet fmt check-fmt run clean

# ci must pass before any commit.
ci: check-fmt vet lint build test

build:
	$(GO) build ./...

test:
	$(GO) test ./...

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
