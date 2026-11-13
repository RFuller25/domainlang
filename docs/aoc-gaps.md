# Known limits

[aoc-toolbox.md](aoc-toolbox.md) maps the usual Advent of Code helper library
onto Domain. This page is the other side: the things Domain cannot express, or
can express only with a workaround, and what to do instead.

## Summary

| Limit | What it affects | What to do |
|---|---|---|
| [No recursive data](#no-recursive-data) | nested lists of unknown depth, trees | no general workaround |
| [No recursion](#no-recursion) | "solve the smaller problem first" | `Domain Expansion: Explore` |
| [No functions as values](#no-functions-as-values) | `map`/`filter` inside an expression | a pipeline body |
| [Updating a collection can copy it](#updating-a-collection-can-copy-it) | building a map, grid or list one write at a time | put the update in a `Fold`, `While` or `Repeat` |
| [A lambda cannot name a channel](#a-lambda-cannot-name-a-channel) | read-only data needed inside a loop | `Fold` with `From:`, or a global |
| [`Match Pattern` templates are flat](#match-pattern-templates-are-flat) | deeply nested line formats | `Case:`, or split first |

## No recursive data

Every value's shape is known before the program runs, and a type cannot
contain itself. So a value whose depth depends on the input cannot be
represented at all: a list of lists of lists nested arbitrarily deep, a pair
whose halves may themselves be pairs, an expression tree, anything shaped like
JSON.

This is the one limit with no general workaround. Puzzles built around such
data (it is roughly one Advent of Code day a year) are out of reach unless the
structure can be flattened while parsing — for example into a list of
`(depth, value)` pairs.

## No recursion

A [Shikigami](language.md#recursion-is-refused) cannot call itself, and the
expression layer has no user-defined functions.

Most problems that look recursive are searches over a space of states, and
`Domain Expansion: Explore` covers them
([reference](ref-expansions.md)):

| You want | Use |
|---|---|
| can I reach it / in how many steps | `Explore`, with `Mode: Distances` for the step counts |
| the cheapest route | `Mode: Cheapest` or `Mode: Costs`, with a `Cost:` lambda |
| how many ways | `Mode: Tally`, with `Value:` and `Combine:` |

A recurrence along a line — each answer built from the few before it — is a
`Fold` carrying a map:

```domain
Maximum Technique: Fold
    Seed: (xs) -> insert(emptymap(0, 0), 0, 1)
    Using: (acc, j) -> insert(acc, j, getor(acc, j - 1, 0) + getor(acc, j - 2, 0) + getor(acc, j - 3, 0))
```

## No functions as values

No builtin takes a lambda, so an expression cannot map, filter or search a
list it has been handed. Write a
[pipeline body](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
instead: an indented pipeline in place of the lambda. With `Params:` a body
can stand in for a lambda of several parameters too, so a `Fold` whose step
needs to sort or search can be written.

The list operations that need no lambda — `sort`, `unique`, `flatten`, `zip`,
`chunk`, `windows` and others — are ordinary
[builtins](ref-builtins-list.md#first-order-list-operations).

## Updating a collection can copy it

`insert`, `put`, `setat`, `set` and `del` return a new collection and leave
the old one as it was. Taken literally, that means copying the whole
collection for every write — far too slow for a loop that makes thousands of
writes to a large map or grid.

The optimizer removes the copy when it can prove nothing could observe the old
collection, and updates in place:

- in a `Fold` or `Reduce` whose lambda passes the accumulator straight into
  the update;
- in a `While` or `Repeat` whose value is the collection, or a tuple holding
  it, updated by a single-statement body.

Written that way, tens of thousands of writes take milliseconds. `--explain`
reports when an update was made in place.

The copy remains when:

- the loop is a `For` or `For Each`;
- the loop body has more than one statement;
- the body reads the loop's value again *after* the write;
- a list being updated with `set` also has `take`, `drop` or `slice` applied
  to it in the same lambda.

See [optimizer.md](optimizer.md#linear-accumulators--the-pass-that-runs-last)
for the exact conditions and how to write around them.

## A lambda cannot name a channel

A channel is read by a statement with a `From:` argument, not by name inside a
lambda. Inside a loop, the statement that fits is `Fold` with `From:`, which
folds the channel's list into the loop's value. `Combine` replaces the value,
which a loop body rarely allows, since a body must return the type it was
given.

When a loop needs to consult read-only data in an arbitrary way, declare it as
a [global](language.md#cursed-object--globals) above the loop
(`Cursed Object: table Of …`) and read it by name.

A Shikigami body and a `Using:` pipeline body cannot read channels at all.

## `Match Pattern` templates are flat

[`Match Pattern`](match-pattern.md) handles repeated holes, optional and
repeated groups, several alternative templates on one statement (`Case:`), and
matches anywhere in a line (`Mode: Scan`). Two things it does not have:

- **Groups inside groups.** A group may not contain another group.
- **Alternatives inside one template.** A single template describes a single
  shape. Lines of different shapes take one `Case:` template each.

For input nested more deeply than that, split it with `Split` first and match
the pieces.
