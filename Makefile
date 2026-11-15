# Building `domain` with the documentation playground.
#
# The playground (docs/wasm/domain.wasm) is a build artifact deliberately not
# committed to the repo — a stale copy would run different code from the docs
# describing it (see docs/wasm/README.md). `go build ./cmd/domain` alone
# therefore ships a docs site with no Run buttons unless docs/wasm/build.sh
# has already been run. `make build` runs both steps in order.
#
# The CLI is linked with -s -w (no symbol table, no DWARF) and -trimpath, the
# same flags docs/wasm/build.sh and the generated-program build already use:
# about a quarter off the binary. Panic traces still carry function names.

.PHONY: build
build:
	./docs/wasm/build.sh
	go build -trimpath -ldflags="-s -w" -o domain ./cmd/domain

# DEV (domain) and PRD (domainlang) are the same code under two names. DEV is
# where work happens; PRD is made from it, never edited on its own. See
# CLAUDE.md. `check-prd` says whether they agree (it fails if they do not, so
# it can run in CI); `sync-prd` makes PRD match and stages the result without
# committing. PRD is ../domainlang unless PRD=<dir> is given.
PRD ?= ../domainlang

.PHONY: check-prd sync-prd
check-prd:
	./scripts/sync-prd.sh check $(PRD)

sync-prd:
	./scripts/sync-prd.sh sync $(PRD)
