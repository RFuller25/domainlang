// `domain expansion: year of fortune <year>` — a whole Advent of Code year,
// laid out on disk before a single puzzle is played.
//
// The editor (dev_aoc.go) fetches one day at a time, which is the right shape
// for December. This is the right shape for the other eleven months: a folder
// per year, four files per day, ready to code against without a network:
//
//	2017/
//	  d1.md        the puzzle text, as markdown
//	  i1           your puzzle input
//	  o1           the answers the site has accepted from you, one `Part N: x` per line
//	  d1.domain    a starter program, already reading i1
//
// Everything with a URL in it is package aoc, so the rules about being a good
// citizen — cache, identify, never ask for a locked day — apply unchanged. On
// top of them this spaces uncached days out, because twenty-five days is the
// first thing here that asks for fifty pages in a row.
//
// Nothing already on disk is overwritten. A starter program that has been
// worked on is the user's, and rerunning the command to pick up a day that has
// since unlocked, or an answer that has since been accepted, must not cost
// them it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"domain/aoc"
)

// yearOptions are the flags of `year of fortune`.
type yearOptions struct {
	Dir string // where the year's folder goes; "<year>" under the current directory if empty
}

// fortuneGap is the pause between two days that had to be fetched. A var so
// the tests need not wait on it.
var fortuneGap = time.Second

// newYearClient builds the aoc client; a var so tests can point it at a server.
var newYearClient = aoc.NewClient

func parseYearArgs(args []string, now time.Time) (int, yearOptions, error) {
	var opts yearOptions
	var year int
	var haveYear bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--dir" || a == "-d":
			if i+1 >= len(args) {
				return 0, opts, fmt.Errorf("%s needs a path", a)
			}
			i++
			opts.Dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			opts.Dir = strings.TrimPrefix(a, "--dir=")
		case strings.HasPrefix(a, "-"):
			return 0, opts, fmt.Errorf("unknown flag %q", a)
		case haveYear:
			return 0, opts, fmt.Errorf("unexpected extra argument %q", a)
		default:
			n, err := strconv.Atoi(a)
			if err != nil {
				return 0, opts, fmt.Errorf("%q is not a year", a)
			}
			year, haveYear = n, true
		}
	}
	if !haveYear {
		return 0, opts, fmt.Errorf("missing <year>, like: domain expansion: year of fortune %d", aoc.LatestYear(now))
	}
	if year < aoc.FirstYear {
		return 0, opts, fmt.Errorf("year %d — the first Advent of Code was %d", year, aoc.FirstYear)
	}
	if latest := aoc.LatestYear(now); year > latest {
		return 0, opts, fmt.Errorf("year %d has not happened yet — the most recent is %d", year, latest)
	}
	return year, opts, nil
}

// YearOfFortune writes the year's folder and returns the exit code.
func YearOfFortune(year int, opts yearOptions, stdout, stderr io.Writer) int {
	client, err := newYearClient()
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	dir := opts.Dir
	if dir == "" {
		dir = strconv.Itoa(year)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}

	now := time.Now()
	if client.Now != nil {
		now = client.Now()
	}
	var written, kept, failed int
	locked := 0
	for day := 1; day <= aoc.Days(year); day++ {
		if err := aoc.Validate(year, day, now); err != nil {
			// Days unlock in order, so the first locked one ends the loop.
			locked = day
			fmt.Fprintf(stdout, "day %2d  not out yet — %v\n", day, err)
			break
		}
		cached := client.Cached(year, day)
		if !cached && day > 1 && fortuneGap > 0 {
			time.Sleep(fortuneGap)
		}
		p, err := client.Fetch(year, day)
		if err != nil {
			fmt.Fprintf(stderr, "domain: day %d: %v\n", day, err)
			if errors.Is(err, aoc.ErrNoSession) {
				return 1
			}
			failed++
			continue
		}
		w, k, err := writeFortuneDay(dir, p)
		if err != nil {
			fmt.Fprintf(stderr, "domain: day %d: %v\n", day, err)
			failed++
			continue
		}
		written += w
		kept += k
		fmt.Fprintf(stdout, "day %2d  %-32s %d file(s) written, %d kept, %d star(s)\n",
			day, p.Title, w, k, p.Solved())
	}
	fmt.Fprintf(stdout, "\n%s: %d file(s) written, %d already there", dir, written, kept)
	if locked > 0 {
		fmt.Fprintf(stdout, ", days %d–%d locked", locked, aoc.Days(year))
	}
	fmt.Fprintln(stdout)
	if failed > 0 {
		return 1
	}
	return 0
}

// writeFortuneDay writes one day's four files, leaving any that exist alone.
// o<n> is written only once the site has accepted an answer: an empty
// expected-output file would read as "the answer is nothing".
func writeFortuneDay(dir string, p *aoc.Puzzle) (written, kept int, err error) {
	files := []struct {
		name string
		body string
		perm os.FileMode
	}{
		{fmt.Sprintf("d%d.md", p.Day), fortuneMarkdown(p), 0o644},
		{fmt.Sprintf("i%d", p.Day), p.Input, 0o600},
		{fmt.Sprintf("o%d", p.Day), fortuneAnswers(p), 0o600},
		{fmt.Sprintf("d%d.domain", p.Day), fortuneStarter(p), 0o644},
	}
	for _, f := range files {
		if f.body == "" {
			continue
		}
		path := filepath.Join(dir, f.name)
		if _, statErr := os.Stat(path); statErr == nil {
			kept++
			continue
		}
		if !strings.HasSuffix(f.body, "\n") {
			f.body += "\n"
		}
		if err := os.WriteFile(path, []byte(f.body), f.perm); err != nil {
			return written, kept, err
		}
		written++
	}
	return written, kept, nil
}

// fortuneAnswers is the expected output: the shape `Part "N":` prints, so the
// file can be diffed against a run as it stands.
func fortuneAnswers(p *aoc.Puzzle) string {
	var b strings.Builder
	for i, a := range p.Answers {
		fmt.Fprintf(&b, "Part %d: %s\n", i+1, a)
	}
	return b.String()
}

// fortuneStarter is a program that parses nothing and answers nothing yet, but
// loads and runs: it reads the day's input and has a Part block per part.
func fortuneStarter(p *aoc.Puzzle) string {
	return fmt.Sprintf(`# %d day %d: %s
#
#   puzzle:   d%[2]d.md
#   input:    i%[2]d
#   expected: o%[2]d
Cursed Energy: i%[2]d

Part "1":
    Reveal: stdout

Part "2":
    Reveal: stdout
`, p.Year, p.Day, p.Title)
}

// fortuneMarkdown renders the unlocked parts of the description.
func fortuneMarkdown(p *aoc.Puzzle) string {
	var b strings.Builder
	first := true
	for _, part := range p.Parts {
		for _, blk := range part {
			switch blk.Kind {
			case aoc.Heading:
				level := "##"
				if first {
					level, first = "#", false
				}
				fmt.Fprintf(&b, "%s %s\n\n", level, strings.Trim(blk.Text, "- "))
			case aoc.Item:
				fmt.Fprintf(&b, "- %s\n", mdBreaks(blk.Text))
			case aoc.Code:
				fmt.Fprintf(&b, "\n```\n%s\n```\n\n", blk.Text)
			default:
				fmt.Fprintf(&b, "%s\n\n", mdBreaks(blk.Text))
			}
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func mdBreaks(s string) string { return strings.ReplaceAll(s, "\n", "  \n") }
