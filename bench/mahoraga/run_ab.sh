#!/bin/sh
# A/B the search: the same four programs, adapted by two builds of `domain`.
#
# Sequential and one process at a time, on purpose. Two searches on one box
# measure each other's noise, and the whole point of the exercise is a pair of
# numbers that can be compared.
#
#   BEFORE=/path/to/domain AFTER=/path/to/domain ./run_ab.sh
#
# It refuses to start on a box that is not idle. `min` over several runs defends
# against a neighbour that shows up once and does nothing about one that never
# leaves: a leftover process holding a core inflates both sides by the same 5–10%
# and, in a search, changes *which* adaptations survive screening — small ones
# become noise and vanish, large ones lose a quarter of their measured value. The
# artifact is then a recipe that is wrong, not a number that is high. Set
# DOMAIN_BENCH_ANY_LOAD=1 to measure anyway.
set -e
BEFORE=${BEFORE:?set BEFORE to the domain binary to compare against}
AFTER=${AFTER:?set AFTER to the domain binary under test}
RUNS=${RUNS:-7}
SCREEN=${SCREEN:-3}
TIMEOUT=${TIMEOUT:-30s}
cd "$(dirname "$0")"

# busy_cores prints how many cores' worth of work the machine is doing, sampled
# over 400ms of /proc/stat. Fields after "cpu" are user nice system idle iowait …
# so 4 and 5 (1-indexed past the label) are the two kinds of not-working.
busy_cores() {
    [ -r /proc/stat ] || { echo unknown; return; }
    set -- $(awk '/^cpu /{print $2+$3+$4+$5+$6+$7+$8, $5+$6}' /proc/stat)
    t0=$1 i0=$2
    sleep 0.4
    set -- $(awk '/^cpu /{print $2+$3+$4+$5+$6+$7+$8, $5+$6}' /proc/stat)
    ncpu=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
    awk -v t0="$t0" -v i0="$i0" -v t1="$1" -v i1="$2" -v n="$ncpu" \
        'BEGIN { d = t1 - t0; if (d <= 0) { print "unknown"; exit } printf "%.2f", (1 - (i1 - i0) / d) * n }'
}

if [ -z "${DOMAIN_BENCH_ANY_LOAD:-}" ]; then
    busy=$(busy_cores)
    case "$busy" in
    unknown) echo "could not read /proc/stat; measuring without an idle check" >&2 ;;
    *)
        if [ "$(awk -v b="$busy" 'BEGIN { print (b > 0.25) }')" = 1 ]; then
            echo "machine is not idle: $busy cores busy, limit 0.25" >&2
            echo "every number this produces would be inflated by whatever else is running." >&2
            echo "find the process and wait, or set DOMAIN_BENCH_ANY_LOAD=1 to measure anyway." >&2
            exit 1
        fi
        echo "machine is idle ($busy cores busy)"
        ;;
    esac
fi

cases="i03_spiral:i03_spiral.input i05_jumps:i05_jumps.input i06_realloc:i06_realloc.input i15_generators:i15"

for side in before after; do
    bin=$BEFORE
    [ "$side" = after ] && bin=$AFTER
    mkdir -p "$side"
    for case in $cases; do
        name=${case%%:*}
        input=${case#*:}
        echo "=== $side / $name"
        "$bin" expansion: mahoraga "$name.domain" "$input" "$name.expected" \
            --plain --runs "$RUNS" --screen-runs "$SCREEN" --timeout "$TIMEOUT" \
            --out "$side/$name-adapted" --recipe "$side/$name.mahoraga.json" \
            >"$side/$name.log" 2>&1 || echo "  (exit $?)"
        sed -n '/^  \(ADAPTED\|BASELINE\)/,$p' "$side/$name.log" | head -12
    done
done
