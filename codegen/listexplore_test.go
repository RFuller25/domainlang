package codegen_test

import "testing"

// Explore over a state that holds a List: the compiled searches key their
// visited/best/settled/memo maps through the byte encoding (hashKey) and the
// Distances/Costs results are dmLMaps. Every mode, the Until: pruning rule and
// the states returned in BFS / settle order must print exactly what the
// interpreter prints.
var listExploreInputs = []string{"", "7", "0 1 2", "1 2 3 4", "12 3 1 23 12", "1 23 12 3"}

const exploreIntsHead = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nDomain Expansion: Explore\n"

// rotate and reverse of a List<Int> state; list(s, s, ...) repeats a state so
// duplicates have to be recognised.
const exploreRot = "(s) -> list(concat(drop(s, 1), take(s, 1)), reverse(s), s)"

func TestCompiledListStateExploreMatchesInterpreter(t *testing.T) {
	cases := []struct{ name, body string }{
		{"collect", "    Using: " + exploreRot + "\n"},
		{"count", "    Mode: Count\n    Using: " + exploreRot + "\n"},
		{"distances", "    Mode: Distances\n    Using: " + exploreRot + "\n"},
		{"steps", "    Mode: Steps\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"steps seed already hits", "    Mode: Steps\n    Until: (s) -> length(s) >= 0\n    Using: " + exploreRot + "\n"},
		{"steps never hits", "    Mode: Steps\n    Until: (s) -> length(s) > 99\n    Using: " + exploreRot + "\n"},
		{"collect with pruning until", "    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"count with pruning until", "    Mode: Count\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"distances with pruning until", "    Mode: Distances\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"cheapest node cost", "    Mode: Cheapest\n    Cost: (t) -> sum(take(t, 1)) + 1\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"cheapest edge cost", "    Mode: Cheapest\n    Cost: (a, b) -> abs(sum(take(a, 1)) - sum(take(b, 1))) + 1\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"cheapest unreachable", "    Mode: Cheapest\n    Cost: (t) -> 1\n    Until: (s) -> length(s) > 99\n    Using: " + exploreRot + "\n"},
		{"costs node cost", "    Mode: Costs\n    Cost: (t) -> sum(take(t, 1)) + 1\n    Using: " + exploreRot + "\n"},
		{"costs edge cost", "    Mode: Costs\n    Cost: (a, b) -> length(a) + sum(take(b, 1)) % 3\n    Using: " + exploreRot + "\n"},
		{"costs with pruning until", "    Mode: Costs\n    Cost: (t) -> 1\n    Until: (s) -> sum(take(s, 1)) = 3\n    Using: " + exploreRot + "\n"},
		{"tally paths over shrinking lists", "    Mode: Tally\n    Value: (s) -> 1\n    Combine: (a, b) -> a + b\n    Using: (s) -> if length(s) > 0 then list(drop(s, 1), take(s, length(s) - 1)) else take(list(s), 0)\n"},
		{"tally with until leaves", "    Mode: Tally\n    Until: (s) -> length(s) = 1\n    Value: (s) -> sum(s)\n    Combine: (a, b) -> a + b\n    Using: (s) -> if length(s) > 0 then list(drop(s, 1), take(s, length(s) - 1)) else take(list(s), 0)\n"},
		{"tally value of another type", "    Mode: Tally\n    Value: (s) -> list(s)\n    Combine: (a, b) -> concat(a, b)\n    Using: (s) -> if length(s) > 1 then list(drop(s, 1), take(s, length(s) - 1)) else take(list(s), 0)\n"},
		{"states that print alike", "    Using: (s) -> if length(s) < 3 then list(concat(s, list(1, 23)), concat(s, list(12, 3)), concat(s, list(123))) else take(list(s), 0)\n"},
	}
	listParity(t, exploreIntsHead, cases, listExploreInputs)
}

// A Tuple holding a List as the state, built from the seed with Apply.
func TestCompiledTupleWithListStateExploreMatchesInterpreter(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Apply\n    Using: (xs) -> tuple(0, xs)\nDomain Expansion: Explore\n"
	const succ = "    Using: (s) -> if item(s, 0) < 3 then list(tuple(item(s, 0) + 1, reverse(item(s, 1))), tuple(item(s, 0) + 1, concat(drop(item(s, 1), 1), take(item(s, 1), 1))), s) else take(list(s), 0)\n"
	cases := []struct{ name, body string }{
		{"collect", succ},
		{"count", "    Mode: Count\n" + succ},
		{"distances", "    Mode: Distances\n" + succ},
		{"steps", "    Mode: Steps\n    Until: (s) -> item(s, 0) = 3\n" + succ},
		{"cheapest", "    Mode: Cheapest\n    Cost: (t) -> item(t, 0) + 1\n    Until: (s) -> item(s, 0) = 3\n" + succ},
		{"costs", "    Mode: Costs\n    Cost: (a, b) -> item(b, 0)\n" + succ},
		{"tally", "    Mode: Tally\n    Value: (s) -> length(item(s, 1))\n    Combine: (a, b) -> a + b\n    Using: (s) -> if item(s, 0) < 3 then list(tuple(item(s, 0) + 1, reverse(item(s, 1))), tuple(item(s, 0) + 1, concat(drop(item(s, 1), 1), take(item(s, 1), 1)))) else take(list(s), 0)\n"},
	}
	listParity(t, head, cases, listExploreInputs)
}

// A List of Text as the state.
func TestCompiledTextListStateExploreMatchesInterpreter(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Split Text by \" \"\nDomain Expansion: Explore\n"
	cases := []struct{ name, body string }{
		{"collect", "    Using: " + exploreRot + "\n"},
		{"count", "    Mode: Count\n    Using: " + exploreRot + "\n"},
		{"distances", "    Mode: Distances\n    Using: " + exploreRot + "\n"},
		{"steps", "    Mode: Steps\n    Until: (s) -> length(first(s)) = 2\n    Using: " + exploreRot + "\n"},
		{"cheapest", "    Mode: Cheapest\n    Cost: (t) -> length(first(t))\n    Until: (s) -> length(first(s)) = 2\n    Using: " + exploreRot + "\n"},
		{"costs", "    Mode: Costs\n    Cost: (t) -> length(first(t))\n    Using: " + exploreRot + "\n"},
		{"tally", "    Mode: Tally\n    Value: (s) -> length(s)\n    Combine: (a, b) -> a + b\n    Using: (s) -> if length(s) > 1 then list(drop(s, 1), take(s, length(s) - 1)) else take(list(s), 0)\n"},
	}
	listParity(t, head, cases, []string{"a", "a b c", "a,b c a,b", "ab c d ab"})
}
