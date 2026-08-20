// Sending an answer, and remembering what came back.
//
// Submission is the one thing here that is not idempotent, so it is the one
// thing that gets a memory. Two rules, both the site's own and both enforced
// before a request is made rather than after one is refused:
//
//   - An answer that has already been refused is never sent again. It is the
//     same answer; the server's reply cannot have changed; sending it costs
//     somebody else's machine a request to say so a second time.
//   - The countdown after a wrong answer is waited out here. The site tells
//     you how long it is, so knowing and asking anyway is a choice, and it is
//     not the one this makes.
//
// What the site says back is prose, and prose is what is shown — "that's not
// the right answer; your answer is too high" is more useful than a boolean,
// and the high/low hint is half of how a day gets solved.
package aoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VerdictKind is what the site made of an answer.
type VerdictKind int

const (
	// Unknown is a reply this does not recognize. The site's own words are
	// still carried, because a reply nobody anticipated is exactly the case
	// where the reader needs to see it rather than a summary of it.
	Unknown VerdictKind = iota
	// Correct — a star.
	Correct
	// Wrong, possibly with a direction to move in.
	Wrong
	// TooSoon: the countdown after a wrong answer has not run out.
	TooSoon
	// WrongLevel: the part being answered is not the part that is open —
	// usually because it is already solved.
	WrongLevel
)

// Verdict is one submission's outcome.
type Verdict struct {
	Kind VerdictKind
	// Hint is "too high" or "too low" when the site offered one.
	Hint string
	// Wait is how long is left of a countdown, when there is one.
	Wait time.Duration
	// Message is what the site said, as text.
	Message string
	// Answer is what was sent, so a screen showing the verdict can say what it
	// was a verdict on.
	Answer string
	// Sent is false when this was decided here — a repeat of an answer already
	// refused, or a countdown still running — and no request was made.
	Sent bool
}

// Star reports whether this verdict earned one.
func (v Verdict) Star() bool { return v.Kind == Correct }

// Summary is the verdict in one line, for a status bar.
func (v Verdict) Summary() string {
	switch v.Kind {
	case Correct:
		return v.Answer + " is right — that's a star"
	case Wrong:
		if v.Hint != "" {
			return v.Answer + " is wrong (" + v.Hint + ")"
		}
		return v.Answer + " is wrong"
	case TooSoon:
		return "too soon — " + until(v.Wait) + " left to wait"
	case WrongLevel:
		return "that is not the part that is open"
	}
	return firstLine(v.Message)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Submit sends an answer for one part.
//
// It refuses, without asking the server, an answer that has already been
// refused and one sent inside a countdown — see the note at the top of the
// file. Both come back as ordinary verdicts with Sent false, because from the
// caller's point of view the question "is this answer any good" was in fact
// answered.
func (c *Client) Submit(year, day, part int, answer string) (Verdict, error) {
	if part != 1 && part != 2 {
		return Verdict{}, fmt.Errorf("part %d — a day has two parts", part)
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return Verdict{}, errors.New("nothing to submit — the run produced no answer")
	}
	if err := Validate(year, day, c.clock()); err != nil {
		return Verdict{}, err
	}

	st := c.readState(year, day)
	if right, ok := st.Correct[strconv.Itoa(part)]; ok {
		return Verdict{
			Kind: WrongLevel, Answer: answer,
			Message: "part " + strconv.Itoa(part) + " is already solved, and the answer was " + right,
		}, nil
	}
	for _, tried := range st.Wrong[strconv.Itoa(part)] {
		if tried == answer {
			return Verdict{
				Kind: Wrong, Answer: answer,
				Message: "this answer has already been sent and refused — not sending it again",
			}, nil
		}
	}
	if wait := st.NextAllowed.Sub(c.clock()); wait > 0 {
		return Verdict{
			Kind: TooSoon, Wait: wait, Answer: answer,
			Message: "the countdown from the last answer has " + until(wait) + " left on it",
		}, nil
	}

	body, err := c.post(year, day, part, answer)
	if err != nil {
		return Verdict{}, err
	}
	v := parseVerdict(body)
	v.Answer, v.Sent = answer, true
	c.record(year, day, part, v)
	return v, nil
}

// post sends the form the site's answer page expects.
func (c *Client) post(year, day, part int, answer string) (string, error) {
	form := url.Values{"level": {strconv.Itoa(part)}, "answer": {answer}}
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/%d/day/%d/answer", c.BaseURL, year, day),
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	c.decorate(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp.StatusCode, string(page))
	}
	return string(page), nil
}

// parseVerdict reads the answer page.
//
// The site's replies are stable sentences, and have been for a decade, so they
// are matched as sentences. An unrecognized reply is Unknown with the prose
// intact rather than a guess: being told what the server said is always more
// use than being told what this thought it meant.
func parseVerdict(page string) Verdict {
	text := verdictText(page)
	lower := strings.ToLower(text)
	v := Verdict{Message: text}

	switch {
	case strings.Contains(lower, "that's the right answer"):
		v.Kind = Correct
	case strings.Contains(lower, "not the right answer"):
		v.Kind = Wrong
		switch {
		case strings.Contains(lower, "too high"):
			v.Hint = "too high"
		case strings.Contains(lower, "too low"):
			v.Hint = "too low"
		}
		v.Wait = parseWait(lower)
	case strings.Contains(lower, "answer too recently"):
		v.Kind = TooSoon
		v.Wait = parseWait(lower)
	case strings.Contains(lower, "right level"):
		v.Kind = WrongLevel
	}
	return v
}

// verdictText pulls the prose out of the answer page, which is one article
// like the puzzle's own.
func verdictText(page string) string {
	var out []string
	for _, m := range articleRe.FindAllStringSubmatch(page, -1) {
		for _, b := range renderBlocks(m[1]) {
			out = append(out, b.Text)
		}
	}
	if len(out) == 0 {
		// No article at all: not a reply this understands, but the reader is
		// better served by the page stripped of its markup than by nothing.
		return strings.TrimSpace(squeeze(tagRe.ReplaceAllString(page, " ")))
	}
	return strings.Join(out, "\n\n")
}

// waitRe reads the countdown out of "you have 4m 30s left to wait", in any of
// the three shapes the site writes it.
var waitRe = regexp.MustCompile(`(?:(\d+)h )?(?:(\d+)m )?(\d+)s left to wait`)

// parseWait is the countdown the site quoted, or zero.
//
// A wrong answer that quotes no countdown still starts one — the site's own
// "please wait one minute before trying again" — so that minute is assumed
// when a wrong answer says nothing more specific. Assuming it is the safe
// direction: the cost is waiting slightly too long.
func parseWait(lower string) time.Duration {
	if m := waitRe.FindStringSubmatch(lower); m != nil {
		atoi := func(s string) time.Duration {
			n, _ := strconv.Atoi(s)
			return time.Duration(n)
		}
		return atoi(m[1])*time.Hour + atoi(m[2])*time.Minute + atoi(m[3])*time.Second
	}
	if strings.Contains(lower, "please wait") {
		return time.Minute
	}
	return 0
}

// ---------------------------------------------------------------------------
// what has been tried
// ---------------------------------------------------------------------------

// state is a day's submission history, kept beside its cached input.
type state struct {
	// Wrong is every answer the site has refused, keyed by part as a string
	// because that is what JSON object keys are.
	Wrong map[string][]string `json:"wrong,omitempty"`
	// Correct is the accepted answer for a part, once there is one.
	Correct map[string]string `json:"correct,omitempty"`
	// NextAllowed is when the countdown from the last wrong answer runs out.
	NextAllowed time.Time `json:"next_allowed,omitzero"`
}

// Tried is every answer already refused for a part — worth showing beside a
// new one, since "I have submitted that before" is the commonest way an
// afternoon disappears.
func (c *Client) Tried(year, day, part int) []string {
	return c.readState(year, day).Wrong[strconv.Itoa(part)]
}

// Accepted is the answer the site took for a part, if this client has seen it
// accepted. The puzzle page carries the same fact once it is re-fetched; this
// is what knows it in between.
func (c *Client) Accepted(year, day, part int) (string, bool) {
	s, ok := c.readState(year, day).Correct[strconv.Itoa(part)]
	return s, ok
}

func (c *Client) statePath(year, day int) string {
	return c.cachePath(year, day, "state.json")
}

// readState loads a day's history. A missing or unreadable file is an empty
// history: this is a convenience, and failing a submission because a cache
// file was corrupted would be the tail wagging the dog.
func (c *Client) readState(year, day int) state {
	var st state
	b, err := os.ReadFile(c.statePath(year, day))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

// record files a verdict away.
//
// A correct answer also drops the cached puzzle page, because that page is now
// out of date in the one way that matters: it does not have part two on it.
func (c *Client) record(year, day, part int, v Verdict) {
	st := c.readState(year, day)
	key := strconv.Itoa(part)
	switch v.Kind {
	case Correct:
		if st.Correct == nil {
			st.Correct = map[string]string{}
		}
		st.Correct[key] = v.Answer
		st.NextAllowed = time.Time{}
		_ = os.Remove(c.cachePath(year, day, "puzzle.html"))
	case Wrong:
		if st.Wrong == nil {
			st.Wrong = map[string][]string{}
		}
		st.Wrong[key] = append(st.Wrong[key], v.Answer)
		fallthrough
	case TooSoon:
		if v.Wait > 0 {
			st.NextAllowed = c.clock().Add(v.Wait)
		}
	}
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		_ = writeCache(c.statePath(year, day), string(b))
	}
}
