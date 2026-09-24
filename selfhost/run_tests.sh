#!/usr/bin/env bash
# Regression + backend-parity sweep for the Domain-in-Domain lexer and parser.
#
# For each of selfhost/phase1_lexer/lexer.domain and
# selfhost/phase2_parser/parser.domain, runs it over every .domain file in
# testdata/, challenges/, and examples/, under both `domain run` (tree-walking
# interpreter) and a `domain build` compiled binary, and checks:
#   1. neither backend crashes on any real project file
#   2. both backends produce byte-identical output
#
# IMPORTANT: backend parity is necessary but not sufficient — both backends
# share the same source, so an actual bug in the .domain program (e.g. the
# strbuf-not-reset-between-strings bug fixed in this directory's history)
# reproduces identically on both and parity alone won't catch it. Spot-check
# real output (e.g. STRING token text) by hand when changing either program.
#
# Usage: bash selfhost/run_tests.sh   (run from the repo root)
set -euo pipefail
cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

BIN="$WORKDIR/domain"
go build -o "$BIN" ./cmd/domain

# The Go source `domain build` generates for these selfhost programs is one
# very long function (2700+ lines, 600+ nested IIFEs from our IF -> closure
# translation, 29 levels deep) because every `also`-chain and if/then/else
# lowers to a nested closure with no abstraction to break it up. In a
# memory-constrained sandbox (~13GB cgroup ceiling here) the Go compiler's
# default inliner walks and duplicates those closures while building SSA for
# that one function and blows past the ceiling — compile gets OOM-killed, and
# a later `go build` reading the truncated cache entry can even panic in the
# linker. None of this is a bug in the generated program; it is the Go
# compiler's memory scaling on a single pathologically large/nested function.
#
# Disabling inlining removes the duplication (the closures are called
# exactly once anyway, right where they're defined, so inlining buys
# nothing), and a lower GOGC plus single-threaded compilation keep peak RSS
# well under the ceiling. This costs nothing at runtime we care about here:
# these binaries exist to prove backend parity, not to be optimized.
export GOFLAGS="${GOFLAGS:--gcflags=all=-l}"
export GOGC="${GOGC:-20}"
export GOMAXPROCS="${GOMAXPROCS:-1}"

# phase1_lexer/lexer.domain imports selfhost/lib/lex_classify.domain via
# `Inherited Technique: lex_classify` — a shared library, not a copy, so the
# classification rule table (which token a character starts) lives in one
# place instead of six. It isn't beside the importing file, so it's found via
# $DOMAIN_PATH instead of the "library beside a program" default.
export DOMAIN_PATH="$(pwd)/selfhost/lib${DOMAIN_PATH:+:$DOMAIN_PATH}"

run_suite() {
    local name=$1
    local prog=$2
    local prog_bin="$WORKDIR/bin_$name"
    "$BIN" build "$prog" -o "$prog_bin" >/dev/null

    local ok=0 fail=0 mismatch=0
    for f in testdata/*.domain challenges/*.domain examples/*.domain; do
        [ -f "$f" ] || continue
        local out_interp out_bin
        if ! out_interp=$("$BIN" run "$prog" < "$f" 2>/tmp/selfhost_err.txt); then
            fail=$((fail + 1))
            echo "[$name] FAIL (interpreter crashed): $f"
            tail -3 /tmp/selfhost_err.txt
            continue
        fi
        if ! out_bin=$("$prog_bin" < "$f" 2>/tmp/selfhost_err.txt); then
            fail=$((fail + 1))
            echo "[$name] FAIL (compiled binary crashed): $f"
            tail -3 /tmp/selfhost_err.txt
            continue
        fi
        if [ "$out_interp" != "$out_bin" ]; then
            mismatch=$((mismatch + 1))
            echo "[$name] MISMATCH (backends disagree): $f"
            continue
        fi
        ok=$((ok + 1))
    done
    echo "[$name] ok=$ok fail=$fail mismatch=$mismatch"
    [ "$fail" -eq 0 ] && [ "$mismatch" -eq 0 ]
}

status=0
run_suite "lexer"    selfhost/phase1_lexer/lexer.domain        || status=1
run_suite "parser"   selfhost/phase2_parser/parser.domain      || status=1
run_suite "expr"     selfhost/phase2_parser/expr.domain        || status=1
run_suite "eval"     selfhost/phase3_eval/eval.domain          || status=1
run_suite "codegen"  selfhost/phase4_codegen/codegen.domain    || status=1
run_suite "selfhost" selfhost/phase5_selfhost/compile_chain.domain || status=1

exit "$status"
