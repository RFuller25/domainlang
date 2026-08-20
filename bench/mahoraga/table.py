#!/usr/bin/env python3
"""Read the recipes an A/B run wrote and print the comparison table.

    ./table.py before after

Each side is a directory of `<program>.mahoraga.json`. What the table shows is
the two things a reader of an A/B actually wants: the speedup each side found,
and *what* the winning side kept — a search that got faster for reasons nobody
can name is the same liability as a binary that did.
"""
import json
import pathlib
import sys


def load(side):
    out = {}
    for path in sorted(pathlib.Path(side).glob("*.mahoraga.json")):
        out[path.name.removesuffix(".mahoraga.json")] = json.loads(path.read_text())
    return out


def ms(nanos):
    return f"{nanos / 1e6:.2f} ms" if nanos else "—"


def speedup(recipe):
    # A search that kept nothing wrote the baseline binary, whatever the two
    # final measurements of it came to. Reporting their ratio as a speedup is
    # how a table ends up crediting a 4% win to a program nobody changed.
    if recipe.get("reverted_to_baseline") or not kept(recipe):
        return 1.0
    return recipe.get("speedup") or 1.0


def kept(recipe):
    return [a for a in recipe.get("adaptations", []) if a.get("kept")]




def main():
    before_dir, after_dir = sys.argv[1], sys.argv[2]
    before, after = load(before_dir), load(after_dir)

    print(f"| Program | baseline | {before_dir} | {after_dir} | change |")
    print("|---|---:|---:|---:|---:|")
    for name in sorted(set(before) | set(after)):
        b, a = before.get(name), after.get(name)
        base = ms(b["final_baseline"]["mean_nanos"]) if b else "—"
        bs, as_ = (speedup(b) if b else 1.0), (speedup(a) if a else 1.0)
        gain = f"{as_ / bs:.2f}×" if bs else "—"
        print(f"| `{name}` | {base} | {bs:.2f}× | {as_:.2f}× | {gain} |")

    for side, recipes in ((before_dir, before), (after_dir, after)):
        print(f"\n### what {side} kept\n")
        for name, recipe in sorted(recipes.items()):
            adaptations = kept(recipe)
            if not adaptations:
                print(f"- `{name}`: nothing")
                continue
            print(f"- `{name}`:")
            for a in adaptations:
                print(f"    - turn {a['turn']} · {a['id']} — {a['effect_pct']:.1f}% ({a['tier']})")


if __name__ == "__main__":
    main()
