package prims

import (
	"strings"
	"testing"
)

const twoSectionInts = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\\n\"\n"

func TestTakeItem(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Take Item 1\n"
	v, _ := runPipeline(t, src, "a,b,c")
	if v.(string) != "b" {
		t.Fatalf("take item 1: got %v want b", v)
	}
}

func TestTakeItemOutOfRange(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Take Item 9\n"
	_, err := runErr(t, src, "a,b")
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range error, got %v", err)
	}
}

func TestCombineTwoChannels(t *testing.T) {
	src := twoSectionInts +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Maximum Technique: Sum\n" +
		"Channel \"b\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Maximum Technique: Max\n" +
		"Maximum Technique: Combine\n" +
		"    From: a, b\n" +
		"    Using: (a, b) -> a + b\n"
	v, _ := runPipeline(t, src, "1,2,3\n\n10,20")
	if v.(int64) != 26 { // sum(1,2,3)=6 + max(10,20)=20
		t.Fatalf("combine: got %v want 26", v)
	}
}

func TestDifferenceAcrossChannels(t *testing.T) {
	src := twoSectionInts +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"Channel \"b\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"Maximum Technique: Difference\n" +
		"    From: a, b\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1,2,3,4\n\n3,4,5")
	if v.(int64) != 2 { // {1,2,3,4} - {3,4,5} = {1,2}
		t.Fatalf("difference count: got %v want 2", v)
	}
}

// TestDifferenceAcrossSetChannels drives the consumer with channels that are
// already Sets (channelAsSet's *SetValue branch; the List branch is covered
// by TestDifferenceAcrossChannels).
func TestDifferenceAcrossSetChannels(t *testing.T) {
	src := twoSectionInts +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Channeled Energy: Convert To Set\n" +
		"Channel \"b\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Channeled Energy: Convert To Set\n" +
		"Maximum Technique: Difference\n" +
		"    From: a, b\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1,2,3,4,2\n\n3,4,5")
	if v.(int64) != 2 { // {1,2,3,4} - {3,4,5} = {1,2}
		t.Fatalf("set-channel difference count: got %v want 2", v)
	}
}

func TestChannelResolveErrors(t *testing.T) {
	chans := twoSectionInts +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Maximum Technique: Sum\n"

	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"unknown channel",
			chans + "Maximum Technique: Combine\n    From: nope\n    Using: (x) -> x\n",
			"unknown channel",
		},
		{
			"combine arity mismatch",
			chans +
				"Channel \"b\":\n    Cursed Technique: Take Item 1\n    Cursed Technique: Split Text by \",\"\n    Channeled Energy: Convert List to Integers\n    Maximum Technique: Sum\n" +
				"Maximum Technique: Combine\n    From: a, b\n    Using: (x) -> x\n",
			"From: names 2",
		},
		{
			"channel redefined",
			chans + "Channel \"a\":\n    Cursed Technique: Take Item 1\n    Cursed Technique: Split Text by \",\"\n    Channeled Energy: Convert List to Integers\n    Maximum Technique: Sum\n" +
				"Reveal: stdout\n",
			"already defined",
		},
		{
			"combine type error across channel",
			twoSectionInts +
				"Channel \"a\":\n    Cursed Technique: Take Item 0\n    Cursed Technique: Split Text by \",\"\n    Channeled Energy: Convert List to Integers\n" +
				"Channel \"b\":\n    Cursed Technique: Take Item 1\n    Cursed Technique: Split Text by \",\"\n    Channeled Energy: Convert List to Integers\n    Maximum Technique: Sum\n" +
				"Maximum Technique: Combine\n    From: a, b\n    Using: (a, b) -> a + b\n",
			"arithmetic needs Int",
		},
	}
	for _, c := range cases {
		_, err := resolveSrc(t, c.src)
		if err == nil {
			t.Fatalf("%s: expected resolve error", c.name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.name, err.Error(), c.want)
		}
	}
}

// TestChannelDoesNotChangeCurrentValue confirms sibling channels both branch
// from the same upstream value (the channel node is a passthrough).
func TestChannelPassthrough(t *testing.T) {
	src := twoSectionInts +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"Cursed Technique: Take Item 1\n" // operates on the still-current List<Text>
	v, _ := runPipeline(t, src, "first\n\nsecond")
	if v.(string) != "second" {
		t.Fatalf("expected passthrough current value, got %v", v)
	}
}
