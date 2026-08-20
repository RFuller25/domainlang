package aoc

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// answerPage wraps a reply in the markup the site's answer page uses.
func answerPage(said string) string {
	return `<html><body><main><article><p>` + said + `</p></article></main></body></html>`
}

const (
	saidRight   = `That's the right answer! You are <span class="day-success">one gold star</span> closer to restoring snow operations.`
	saidTooHigh = `That's not the right answer; your answer is too high. Please wait one minute before trying again.`
	saidTooSoon = `You gave an answer too recently; you have to wait after submitting an answer before trying again. You have 4m 30s left to wait.`
	saidLevel   = `You don't seem to be solving the right level. Did you already complete it?`
)

// submitting stands up a fake answer page and reports what was posted to it.
func submitting(t *testing.T, reply string) (*Client, *url.Values, *int) {
	t.Helper()
	var posted url.Values
	var count int
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_, _ = w.Write([]byte(pageOnePart))
			return
		}
		count++
		_ = r.ParseForm()
		posted = r.PostForm
		_, _ = w.Write([]byte(answerPage(reply)))
	})
	return c, &posted, &count
}

func TestSubmitSendsTheAnswerAndReadsTheStar(t *testing.T) {
	c, posted, _ := submitting(t, saidRight)
	v, err := c.Submit(2023, 7, 1, "250120186")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Star() {
		t.Errorf("verdict = %d, want Correct (%q)", v.Kind, v.Message)
	}
	if got := posted.Get("level"); got != "1" {
		t.Errorf("level = %q, want 1", got)
	}
	if got := posted.Get("answer"); got != "250120186" {
		t.Errorf("answer = %q", got)
	}
	if !strings.Contains(v.Summary(), "star") {
		t.Errorf("summary = %q", v.Summary())
	}
}

// A correct answer makes the cached page out of date in the one way that
// matters: part two is on it now, and was not before.
func TestACorrectAnswerDropsTheCachedPage(t *testing.T) {
	c, _, _ := submitting(t, saidRight)
	if _, err := c.Fetch(2023, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.cachePath(2023, 7, "puzzle.html")); err != nil {
		t.Fatalf("the page was not cached to begin with: %v", err)
	}
	if _, err := c.Submit(2023, 7, 1, "250120186"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.cachePath(2023, 7, "puzzle.html")); !os.IsNotExist(err) {
		t.Error("the cached page survived a correct answer, so part two would stay hidden")
	}
}

func TestAWrongAnswerCarriesTheDirection(t *testing.T) {
	c, _, _ := submitting(t, saidTooHigh)
	v, err := c.Submit(2023, 7, 1, "99999")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != Wrong {
		t.Fatalf("verdict = %d, want Wrong (%q)", v.Kind, v.Message)
	}
	if v.Hint != "too high" {
		t.Errorf("hint = %q, want %q", v.Hint, "too high")
	}
	if v.Wait != time.Minute {
		t.Errorf("wait = %s, want the minute the site asks for", v.Wait)
	}
	if !strings.Contains(v.Summary(), "too high") {
		t.Errorf("summary = %q", v.Summary())
	}
}

// The same wrong answer is never sent twice. The server's reply cannot have
// changed, and asking again is a request somebody else's machine has to serve.
func TestAnAnswerAlreadyRefusedIsNotSentAgain(t *testing.T) {
	c, _, count := submitting(t, saidTooHigh)
	if _, err := c.Submit(2023, 7, 1, "99999"); err != nil {
		t.Fatal(err)
	}
	// Past the countdown the first answer started, so that it is the memory of
	// the answer being refused.
	c.Now = func() time.Time { return december.Add(time.Hour) }

	v, err := c.Submit(2023, 7, 1, "99999")
	if err != nil {
		t.Fatal(err)
	}
	if *count != 1 {
		t.Errorf("the site was asked %d times about the same answer, want 1", *count)
	}
	if v.Kind != Wrong || v.Sent {
		t.Errorf("verdict = %d, sent = %v — want a wrong answer decided locally", v.Kind, v.Sent)
	}
	if tried := c.Tried(2023, 7, 1); len(tried) != 1 || tried[0] != "99999" {
		t.Errorf("tried = %v, want the one refused answer", tried)
	}
}

// The countdown the site quotes is waited out here rather than walked into.
func TestTheCountdownIsHeldToLocally(t *testing.T) {
	c, _, count := submitting(t, saidTooHigh)
	if _, err := c.Submit(2023, 7, 1, "1"); err != nil {
		t.Fatal(err)
	}
	v, err := c.Submit(2023, 7, 1, "2")
	if err != nil {
		t.Fatal(err)
	}
	if *count != 1 {
		t.Errorf("a second answer was posted inside the countdown (%d posts)", *count)
	}
	if v.Kind != TooSoon {
		t.Fatalf("verdict = %d, want TooSoon (%q)", v.Kind, v.Message)
	}
	if v.Wait <= 0 || v.Wait > time.Minute {
		t.Errorf("wait = %s, want what is left of the minute", v.Wait)
	}

	// Once it has run out, the answer goes through.
	c.Now = func() time.Time { return december.Add(2 * time.Minute) }
	if _, err := c.Submit(2023, 7, 1, "2"); err != nil {
		t.Fatal(err)
	}
	if *count != 2 {
		t.Errorf("the answer was still held after the countdown ran out")
	}
}

func TestSubmitReadsTheSitesOwnCountdown(t *testing.T) {
	c, _, _ := submitting(t, saidTooSoon)
	v, err := c.Submit(2023, 7, 2, "12")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != TooSoon {
		t.Fatalf("verdict = %d, want TooSoon", v.Kind)
	}
	if v.Wait != 4*time.Minute+30*time.Second {
		t.Errorf("wait = %s, want 4m30s", v.Wait)
	}
}

func TestSubmitRecognizesTheWrongLevel(t *testing.T) {
	c, _, _ := submitting(t, saidLevel)
	v, err := c.Submit(2023, 7, 1, "12")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != WrongLevel {
		t.Errorf("verdict = %d, want WrongLevel (%q)", v.Kind, v.Message)
	}
}

// A part with an accepted answer is not submitted to again — the site would
// only say "you don't seem to be solving the right level", and it already
// knows.
func TestASolvedPartIsNotSubmittedTo(t *testing.T) {
	c, _, count := submitting(t, saidRight)
	if _, err := c.Submit(2023, 7, 1, "250120186"); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Accepted(2023, 7, 1); !ok || got != "250120186" {
		t.Errorf("accepted = %q (%v), want the answer that was taken", got, ok)
	}
	v, err := c.Submit(2023, 7, 1, "somethingelse")
	if err != nil {
		t.Fatal(err)
	}
	if *count != 1 {
		t.Errorf("a solved part was submitted to again (%d posts)", *count)
	}
	if v.Kind != WrongLevel || !strings.Contains(v.Message, "250120186") {
		t.Errorf("verdict = %d %q, want it to say the part is done and what took it", v.Kind, v.Message)
	}
}

// A reply nobody anticipated is reported as the site's own words rather than
// as a guess about what they meant.
func TestAnUnrecognizedReplyKeepsItsWords(t *testing.T) {
	c, _, _ := submitting(t, "Something entirely new happened.")
	v, err := c.Submit(2023, 7, 1, "12")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != Unknown {
		t.Errorf("verdict = %d, want Unknown", v.Kind)
	}
	if !strings.Contains(v.Message, "entirely new") {
		t.Errorf("message = %q, want the site's own words", v.Message)
	}
}

func TestSubmitRefusesNonsense(t *testing.T) {
	c, _, count := submitting(t, saidRight)
	if _, err := c.Submit(2023, 7, 3, "12"); err == nil {
		t.Error("part 3 was accepted")
	}
	if _, err := c.Submit(2023, 7, 1, "   "); err == nil {
		t.Error("an empty answer was accepted")
	}
	if _, err := c.Submit(2023, 26, 1, "12"); err == nil {
		t.Error("day 26 was accepted")
	}
	if *count != 0 {
		t.Errorf("%d of those reached the site", *count)
	}
}
