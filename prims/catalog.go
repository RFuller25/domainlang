package prims

import "domain/ast"

// PrimDoc is human-facing documentation for one primitive: enough for an
// editor to render a signature and a one-line summary on hover and in
// completion. The summaries are distilled from docs/primitives.md; the
// DocAnchor links back into that page (served by `domain expansion:
// documentation`) for the full reference.
type PrimDoc struct {
	ID        string // matches Primitive.ID
	Keyword   string // the themed keyword this primitive lives under
	Signature string // type step, e.g. "List<T> → List<List<T>>"
	Summary   string // one sentence, plain text
	DocAnchor string // heading slug in primitives.md, e.g. "window"
}

// Catalog documents every primitive in Registry, keyed by ID. A test
// (catalog_test.go) pins it to exactly the registered set, so a new primitive
// cannot ship without its documentation.
var Catalog = map[string]PrimDoc{
	// Cursed Energy — sources
	"Read Source": {"Read Source", "Cursed Energy", "(nothing) → Text",
		"Reads the named file (falling back to stdin when it is absent); must be the first stage.", "read-source"},

	// Cursed Technique — transforms
	"Split": {"Split", "Cursed Technique", "Text → List<Text>",
		"Splits text on a required separator string (an empty separator splits into characters).", "split"},
	"Split Each": {"Split Each", "Cursed Technique", "List<Text> → List<List<Text>>",
		"Split applied to every element of a list.", "split-each"},
	"Split Fields": {"Split Fields", "Cursed Technique", "Text → List<Text> | List<Text> → List<List<Text>>",
		"Splits on runs of whitespace, discarding empty fields — the classic fields() helper.", "split-fields"},
	"Extract Integers": {"Extract Integers", "Cursed Technique", "Text → List<Int> | List<Text> → List<List<Int>>",
		"Mines every integer out of messy text — the AoC parse-ints workhorse.", "extract-integers"},
	"Ragged Columns": {"Ragged Columns", "Cursed Technique", "List<Text> → List<List<Text>>",
		"The character columns of a block of lines, tolerating ragged line lengths.", "ragged-columns"},
	"Window": {"Window", "Cursed Technique", "List<T> → List<List<T>>",
		"Fully-contained sliding windows of a fixed size (with an optional step).", "window"},
	"Flatten": {"Flatten", "Cursed Technique", "List<List<T>> → List<T>",
		"Concatenates the groups in order.", "flatten"},
	"Enumerate": {"Enumerate", "Cursed Technique", "List<T> → List<(Int, T)>",
		"Pairs every element with its 0-based index.", "enumerate"},
	"Map Each": {"Map Each", "Cursed Technique", "List<T> × (T → U) → List<U>",
		"Applies the Using: lambda to every element.", "map-each"},
	"Filter": {"Filter", "Cursed Technique", "List<T> × (T → Bool) → List<T>",
		"Keeps the elements for which the Using: predicate is true.", "filter"},
	"Unique": {"Unique", "Cursed Technique", "List<T> → List<T>",
		"Order-preserving deduplication (first occurrence wins); T must be keyable.", "unique"},
	"Match Pattern": {"Match Pattern", "Cursed Technique", "Text → V | List<Text> → List<V>",
		"Parses each line against a typed-hole template into a Record, Tuple, or List.", "match-pattern"},
	"Take Item": {"Take Item", "Cursed Technique", "List<T> → T",
		"Picks the element at a 0-based index (out of range is a runtime error).", "take-item"},
	"Map Cells": {"Map Cells", "Cursed Technique", "Grid<T> × (T → U) → Grid<U>",
		"Transforms every grid cell; a 3-parameter lambda binds (grid, row, col).", "map-cells"},
	"Find Cells": {"Find Cells", "Cursed Technique", "Grid<T> × (T → Bool) → List<(Int, Int)>",
		"The (row, col) positions of every cell satisfying the predicate, row-major.", "find-cells"},
	"Transpose": {"Transpose", "Cursed Technique", "Grid<T> → Grid<T>",
		"Swaps rows and columns.", "transpose"},
	"Apply": {"Apply", "Cursed Technique", "T × (T → U) → U",
		"The scalar analogue of Map Each: transforms the whole current value.", "apply"},

	// Channeled Energy — coercions
	"Convert To Integers": {"Convert To Integers", "Channeled Energy", "List<Text> → List<Int> | List<List<Text>> → List<List<Int>>",
		"Parses each field to an Int (form chosen by input type).", "convert-to-integers"},
	"Convert To Floats": {"Convert To Floats", "Channeled Energy", "List<Text|Int> → List<Float> | nested",
		"Parses or widens each field to a Float (form chosen by input type).", "convert-to-floats"},
	"Convert To Grid": {"Convert To Grid", "Channeled Energy", "List<List<T>> | List<Text> | Sparse<T> → Grid<T>",
		"Materializes a dense Grid from rows, characters, or a sparse plane.", "convert-to-grid"},
	"Convert To Sparse Grid": {"Convert To Sparse Grid", "Channeled Energy", "… → Sparse<T>",
		"Builds an unbounded default-valued plane from a grid, map, or list of points.", "convert-to-sparse-grid"},
	"Convert To Set": {"Convert To Set", "Channeled Energy", "List<T> → Set<T>",
		"Deduplicates a list into a Set, preserving first-seen order; T must be keyable.", "convert-to-set"},

	// Maximum Technique — reductions
	"Sum": {"Sum", "Maximum Technique", "List<Int> → Int",
		"Sum of all elements (0 for the empty list).", "sum"},
	"Sum Each Group": {"Sum Each Group", "Maximum Technique", "List<List<Int>> → List<Int>",
		"Per-group sums, preserving order.", "sum-each-group"},
	"Max": {"Max", "Maximum Technique", "List<Int> → Int",
		"The largest element (the empty list is a runtime error).", "max--min--product"},
	"Min": {"Min", "Maximum Technique", "List<Int> → Int",
		"The smallest element (the empty list is a runtime error).", "max--min--product"},
	"Product": {"Product", "Maximum Technique", "List<Int> → Int",
		"The product of all elements (the empty list is a runtime error).", "max--min--product"},
	"Count": {"Count", "Maximum Technique", "List<T> | Set<T> → Int",
		"Cardinality — how many elements.", "count"},
	"Count Matching": {"Count Matching", "Maximum Technique", "List<T> × (T → Bool) → Int",
		"How many elements satisfy the predicate.", "count-matching"},
	"Count Cells": {"Count Cells", "Maximum Technique", "Grid<T> × (T → Bool) → Int",
		"How many grid cells satisfy the predicate (3-param lambda binds coordinates).", "count-cells"},
	"Select Top K": {"Select Top K", "Maximum Technique", "List<Int> → List<Int> (or → Int with , Sum)",
		"Takes the first K elements of the ordered list; fuses with Sort into a quickselect.", "select-top-k"},
	"Fold": {"Fold", "Maximum Technique", "List<T> × Seed × (Acc, T → Acc) → Acc",
		"Left fold from a Seed accumulator; also a channel consumer with From:.", "fold"},
	"Join": {"Join", "Maximum Technique", "List<Text> → Text",
		"Concatenates the elements, with an optional separator.", "join"},
	"Group By": {"Group By", "Maximum Technique", "List<T> × (T → K) → Map<K, List<T>>",
		"Buckets elements by the lambda's key; K must be keyable.", "group-by"},
	"Count By": {"Count By", "Maximum Technique", "List<T> × (T → K) → Map<K, Int>",
		"Frequency map of the lambda's key; K must be keyable.", "count-by"},
	"Min By": {"Min By", "Maximum Technique", "List<T> × (T → Int) → T",
		"The element whose Int key is smallest (first wins ties).", "min-by--max-by"},
	"Max By": {"Max By", "Maximum Technique", "List<T> × (T → Int) → T",
		"The element whose Int key is largest (first wins ties).", "min-by--max-by"},
	"Intersect": {"Intersect", "Maximum Technique", "List<List<T>> → Set<T>",
		"Set intersection over the groups (T keyable).", "intersect--union"},
	"Union": {"Union", "Maximum Technique", "List<List<T>> → Set<T>",
		"Set union over the groups, deduplicated left-to-right (T keyable).", "intersect--union"},
	"Difference": {"Difference", "Maximum Technique", "List<List<T>> → Set<T> (or channel consumer)",
		"Elements of the first group not present in any later group; also a two-channel consumer.", "difference--channel-consumer--set-t-t-keyable"},
	"Merge Ranges": {"Merge Ranges", "Maximum Technique", "List<(Int, Int)> → List<(Int, Int)>",
		"Coalesces overlapping or adjacent inclusive integer intervals.", "merge-ranges--coalesce-inclusive-integer-intervals"},

	// Domain Expansion — swappable algorithms
	"Sort": {"Sort", "Domain Expansion", "List<Int> → List<Int>",
		"Sorts ascending (Descending flips it); the optimizer may substitute the algorithm.", "sort--quicksort"},
	"Sort By": {"Sort By", "Domain Expansion", "List<T> × (T → Int) → List<T>",
		"Stable sort by the lambda's Int key, ascending by default.", "sort-by"},
	"All Pairs": {"All Pairs", "Domain Expansion", "List<T> × Mode × (a, b → …) → …",
		"Visits every pair; Mode picks Filter/Count/First/Map. Sum-to-constant is optimized to O(n).", "all-pairs--combinations-k"},
	"Combinations": {"Combinations", "Domain Expansion", "List<T> × Mode × (k params → …) → …",
		"Visits every k-combination in lexicographic order; Mode picks the result shape.", "all-pairs--combinations-k"},
	"Permutations": {"Permutations", "Domain Expansion", "List<T> → List<List<T>>",
		"Every ordering of the list (bounded: more than 9 elements errors).", "permutations"},
	"Subsets": {"Subsets", "Domain Expansion", "List<T> → List<List<T>>",
		"The power set (bounded: more than 16 elements errors).", "subsets"},
	"BFS": {"BFS", "Domain Expansion", "Grid<T> × (T → Bool) → Grid<Int>",
		"Breadth-first step distances from a start over walkable cells (unreachable = -1).", "bfs"},
	"Dijkstra": {"Dijkstra", "Domain Expansion", "Grid<Int> → Grid<Int>",
		"Minimum entry-cost from a start to every cell (4-connectivity, min-heap).", "dijkstra"},
	"Flood Fill": {"Flood Fill", "Domain Expansion", "Grid<T> × (T → Bool) → Grid<Int>",
		"Marks the start's 4-connected matching region: 1 inside, 0 elsewhere.", "flood-fill"},
	"Connected Components": {"Connected Components", "Domain Expansion", "Grid<T> × (T → Bool) → Int",
		"How many 4-connected regions of matching cells the grid contains (union-find).", "connected-components"},

	// Reverse Cursed Technique — inversions
	"Reverse": {"Reverse", "Reverse Cursed Technique", "List<T> → List<T>",
		"Reverses element order.", "reverse"},

	// Sinks and assertions
	"Emit": {"Emit", "Reveal", "T → (output)",
		"Writes the current value to the output sink (Reveal: stdout).", "simple-domain-channel-shikigami-binding-vow-reveal"},
	"Binding Vow": {"Binding Vow", "Binding Vow", "T → T (asserts)",
		"Asserts an invariant about the current value; stripped under --release.", "simple-domain-channel-shikigami-binding-vow-reveal"},
}

// Doc returns the documentation for a primitive ID.
func Doc(id string) (PrimDoc, bool) {
	d, ok := Catalog[id]
	return d, ok
}

// Lookup identifies the primitive a statement resolves to, without running the
// full type checker — enough for an editor to document the operation on a line
// even when the program does not yet type-check. It returns nil when no
// primitive matches the statement's keyword and operation phrase.
func Lookup(stmt *ast.Statement) *Primitive {
	return findPrimitive(stmt)
}
