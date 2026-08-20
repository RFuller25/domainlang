package aoc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// december is a fixed clock inside an event, so every test below can ask for a
// puzzle without waiting for December to come round.
var december = time.Date(2023, time.December, 26, 12, 0, 0, 0, time.UTC)

// pageOnePart is a puzzle page as the site serves one before part one is
// solved: one article, a title, a worked example, and no answers.
const pageOnePart = `<!DOCTYPE html><html><head><title>Day 7</title></head><body>
<main>
<article class="day-desc"><h2>--- Day 7: Camel Cards ---</h2>
<p>Your all-expenses-paid trip turns out to be a <em>ride</em> in an airship.
Because the journey is long, you can play a game: <code>Camel Cards</code>.</p>
<p>For example:</p>
<pre><code>32T3K 765
T55J5 684
KK677 28
</code></pre>
<p>What are the <em>total winnings</em>? A card is worth &gt; 0 &amp; &lt; 100.</p>
<ul><li>the first hand is a pair</li><li>the second is three of a kind</li></ul>
</article>
<p>To play, please identify yourself via one of these services:</p>
</main></body></html>`

// pageTwoParts is the same day once part one is solved: a second article, and
// the answer the site accepted written into the page.
const pageTwoParts = `<!DOCTYPE html><html><body><main>
<article class="day-desc"><h2>--- Day 7: Camel Cards ---</h2>
<p>The first part.</p>
<pre><code>32T3K 765
T55J5 684
</code></pre>
</article>
<p>Your puzzle answer was <code>250120186</code>.</p>
<article class="day-desc"><h2>--- Part Two ---</h2>
<p>Now, <em>J</em> cards are jokers.</p>
</article>
<p>Both parts of this puzzle are complete! They provide two gold stars: **</p>
</main></body></html>`

// testClient is a client pointed at a fake site and a temporary cache, which
// is every test here: nothing below touches the network or the real cache.
func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		Session:  "cookie-value",
		BaseURL:  srv.URL,
		CacheDir: t.TempDir(),
		HTTP:     srv.Client(),
		Now:      func() time.Time { return december },
	}, srv
}

// ---------------------------------------------------------------------------
// the session cookie
// ---------------------------------------------------------------------------

func TestCleanSessionTakesWhatWasPasted(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc123", "abc123"},
		{"  abc123\n", "abc123"},
		{"session=abc123", "abc123"},
		{"session=abc123; Path=/", "abc123"},
		{"", ""},
		{"\n\t ", ""},
	}
	for _, c := range cases {
		if got := cleanSession(c.in); got != c.want {
			t.Errorf("cleanSession(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNoSessionSaysHowToGetOne(t *testing.T) {
	t.Setenv("AOC_SESSION", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := findSession(); err == nil {
		t.Fatal("found a session cookie where none was planted")
	} else if !strings.Contains(err.Error(), "AOC_SESSION") {
		t.Errorf("the error does not say where to put a cookie:\n%v", err)
	}
}

func TestSessionComesFromTheConfigFile(t *testing.T) {
	config := t.TempDir()
	t.Setenv("AOC_SESSION", "")
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(filepath.Join(config, "domain"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "domain", "aoc-session"), []byte("from-a-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := findSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-a-file" {
		t.Errorf("session = %q, want %q", got, "from-a-file")
	}
}

// The User-Agent says what this is, and carries a contact when one is
// configured — which is what the site's automation guidelines ask for.
func TestUserAgentIdentifiesTheTool(t *testing.T) {
	c := &Client{}
	if ua := c.userAgent(); !strings.Contains(ua, "domain") || !strings.Contains(ua, "github.com") {
		t.Errorf("User-Agent does not identify the tool: %q", ua)
	}
	c.Contact = "someone@example.com"
	if ua := c.userAgent(); !strings.Contains(ua, "someone@example.com") {
		t.Errorf("User-Agent dropped the contact: %q", ua)
	}
}

// ---------------------------------------------------------------------------
// what may be asked for
// ---------------------------------------------------------------------------

func TestValidateRefusesWhatIsNotAPuzzle(t *testing.T) {
	cases := []struct {
		year, day int
		want      string
	}{
		{2023, 0, "day 1 to day 25"},
		{2023, 26, "day 1 to day 25"},
		{2014, 1, "first Advent of Code"},
		{2099, 1, "has not happened yet"},
	}
	for _, c := range cases {
		err := Validate(c.year, c.day, december)
		if err == nil {
			t.Errorf("Validate(%d, %d) allowed it", c.year, c.day)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Validate(%d, %d) = %v, want something about %q", c.year, c.day, err, c.want)
		}
	}
	if err := Validate(2023, 7, december); err != nil {
		t.Errorf("Validate(2023, 7) refused a puzzle that exists: %v", err)
	}
}

// A day is refused until it unlocks, with the time it does — the whole point
// being not to ask the server for something it cannot have.
func TestValidateWaitsForTheUnlock(t *testing.T) {
	// One hour before midnight EST on the 7th.
	justBefore := Unlock(2023, 7).Add(-time.Hour)
	err := Validate(2023, 7, justBefore)
	if err == nil {
		t.Fatal("day 7 was allowed an hour before it unlocked")
	}
	if !strings.Contains(err.Error(), "unlocks") {
		t.Errorf("error does not mention the unlock: %v", err)
	}
	if err := Validate(2023, 7, Unlock(2023, 7)); err != nil {
		t.Errorf("day 7 refused at the moment it unlocked: %v", err)
	}
}

// Before December a year has no puzzles in it, which is the difference between
// the latest year and the current one.
func TestLatestYearWaitsForDecember(t *testing.T) {
	if got := LatestYear(time.Date(2024, time.November, 30, 23, 0, 0, 0, time.UTC)); got != 2023 {
		t.Errorf("LatestYear(November 2024) = %d, want 2023", got)
	}
	if got := LatestYear(time.Date(2024, time.December, 1, 5, 0, 0, 0, time.UTC)); got != 2024 {
		t.Errorf("LatestYear(December 2024) = %d, want 2024", got)
	}
}

// ---------------------------------------------------------------------------
// fetching
// ---------------------------------------------------------------------------

func TestFetchReadsThePuzzleAndTheInput(t *testing.T) {
	var gotCookie, gotAgent string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAgent = r.Header.Get("User-Agent")
		if ck, err := r.Cookie("session"); err == nil {
			gotCookie = ck.Value
		}
		if strings.HasSuffix(r.URL.Path, "/input") {
			_, _ = w.Write([]byte("32T3K 765\nT55J5 684\n"))
			return
		}
		_, _ = w.Write([]byte(pageOnePart))
	})

	p, err := c.Fetch(2023, 7)
	if err != nil {
		t.Fatal(err)
	}
	if gotCookie != "cookie-value" {
		t.Errorf("session cookie = %q, want it sent", gotCookie)
	}
	if !strings.Contains(gotAgent, "domain") {
		t.Errorf("User-Agent = %q", gotAgent)
	}
	if p.Title != "Camel Cards" {
		t.Errorf("title = %q, want %q", p.Title, "Camel Cards")
	}
	if p.Unlocked() != 1 {
		t.Errorf("unlocked = %d, want 1", p.Unlocked())
	}
	if p.Solved() != 0 {
		t.Errorf("solved = %d, want 0", p.Solved())
	}
	if p.Input != "32T3K 765\nT55J5 684\n" {
		t.Errorf("input = %q", p.Input)
	}
	if !strings.HasPrefix(p.Example, "32T3K 765") {
		t.Errorf("example = %q, want the worked example", p.Example)
	}
}

// The input is fetched once, ever. It cannot change, and the site asks not to
// be asked twice — so the second Fetch must make no request at all.
func TestFetchIsCached(t *testing.T) {
	var requests int
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.HasSuffix(r.URL.Path, "/input") {
			_, _ = w.Write([]byte("input\n"))
			return
		}
		_, _ = w.Write([]byte(pageOnePart))
	})

	if _, err := c.Fetch(2023, 7); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("first fetch made %d requests, want 2 (the page and the input)", requests)
	}
	if !c.Cached(2023, 7) {
		t.Error("the day is not reported as cached after fetching it")
	}
	if _, err := c.Fetch(2023, 7); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Errorf("the second fetch made %d more requests, want none", requests-2)
	}
}

// Refresh is for the page that grew a second half. It re-reads the page and
// leaves the input alone.
func TestRefreshRereadsThePageOnly(t *testing.T) {
	var pages, inputs int
	page := pageOnePart
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/input") {
			inputs++
			_, _ = w.Write([]byte("input\n"))
			return
		}
		pages++
		_, _ = w.Write([]byte(page))
	})

	if _, err := c.Fetch(2023, 7); err != nil {
		t.Fatal(err)
	}
	page = pageTwoParts
	p, err := c.Refresh(2023, 7)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 {
		t.Errorf("the page was fetched %d times, want 2", pages)
	}
	if inputs != 1 {
		t.Errorf("the input was fetched %d times, want 1 — it cannot have changed", inputs)
	}
	if p.Unlocked() != 2 {
		t.Errorf("unlocked = %d, want 2 after the refresh", p.Unlocked())
	}
	if got, ok := p.Answer(1); !ok || got != "250120186" {
		t.Errorf("answer for part 1 = %q (%v), want the one on the page", got, ok)
	}
}

// A cached input is somebody's personal file, fetched with their login. It is
// written where only they can read it.
func TestCachedFilesArePrivate(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(pageOnePart))
	})
	if _, err := c.Fetch(2023, 7); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(c.cachePath(2023, 7, "input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("the cached input is %o, want 600 — it is a personal file", perm)
	}
}

// An expired cookie comes back as a 400 and a page of prose, which would send
// a reader looking for the wrong problem if it were reported as a status code.
func TestAnExpiredCookieSaysSo(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Please log in to get your puzzle input."))
	})
	_, err := c.Fetch(2023, 7)
	if err == nil {
		t.Fatal("a 400 was not an error")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want it to name the expired cookie", err)
	}
}
