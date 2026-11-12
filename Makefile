# Building `domain` with the documentation playground.
#
# The playground (docs/wasm/domain.wasm) is a build artifact deliberately not
# committed to the repo — a stale copy would run different code from the docs
# describing it (see docs/wasm/README.md). `go build ./cmd/domain` alone
# therefore ships a docs site with no Run buttons unless docs/wasm/build.sh
# has already been run. `make build` runs both steps in order.

# The CLI is linked with -s -w (no symbol table, no DWARF) and -trimpath, the
# same flags docs/wasm/build.sh and the generated-program build already use.

.PHONY: build
build:
	./docs/wasm/build.sh
	go build -trimpath -ldflags="-s -w" -o domain ./cmd/domain
