// Package aoc talks to adventofcode.com: one day's puzzle text, the input
// that belongs to the person asking, and the answer they want to check.
//
// It exists because the editor's loop for an AoC day was three windows wide.
// Read the puzzle in a browser, save the input out of another tab, alt-tab to
// the terminal to run the program, alt-tab back to paste the answer into the
// form. Everything but the middle step is fetching a URL with a cookie on it,
// which is a thing a program can do — so `domain expansion: development` does
// it (cmd/domain/dev_aoc.go), and this is the part of that with no terminal in
// it, which is the part worth testing.
//
// # Being a good citizen
//
// Advent of Code is one person's server and it asks automation to behave. Four
// rules, and each one is a decision here rather than a note in a README:
//
//   - Cache, and never fetch what is already cached. Inputs never change, so an
//     input is fetched exactly once per machine, ever. The puzzle page does
//     change — part two appears when part one is solved — so it is re-fetched
//     only when asked for, or when a correct submission has just changed it.
//   - Say who you are. Every request carries a User-Agent naming this tool and
//     where it lives; `AOC_CONTACT` adds the address the guidelines ask for.
//   - Do not ask for a puzzle that does not exist yet. Unlock is midnight EST
//     on the day, and a request before it is refused here rather than at the
//     server.
//   - Do not guess at answers. A wrong answer is remembered, so the same one
//     cannot be sent twice, and the countdown the site returns is held to
//     locally rather than being walked into again.
//
// # The session cookie
//
// An input is personal: two people solving the same day get different files,
// and the answer to one is wrong for the other. The site identifies you by a
// session cookie, so that cookie is the one piece of configuration here. It is
// read from `AOC_SESSION`, or from a file — `~/.config/domain/aoc-session`, or
// `~/.adventofcode.session` for anyone who already keeps one there for another
// tool. It is never written anywhere, including into the cache.
package aoc

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client fetches puzzles and submits answers, through a cache.
//
// The zero value is not usable — NewClient discovers the session and the cache
// directory. The exported fields are there so a test can point the whole thing
// at an httptest server and a temporary directory, which is how everything
// below is tested without touching the network.
type Client struct {
	Session  string // the session cookie's value
	BaseURL  string // "https://adventofcode.com", or a test server
	CacheDir string // where fetched pages and inputs are kept
	Contact  string // added to the User-Agent, per the automation guidelines
	HTTP     *http.Client
	// Now is the clock the unlock check and the submission countdown measure
	// against; a field so a test can stand in December without waiting for it.
	Now func() time.Time
}

// site is the real server. It is a constant rather than a default assigned in
// NewClient so that a test that forgets to set BaseURL fails by connecting to
// nothing rather than by quietly hitting the live site.
const site = "https://adventofcode.com"

// ErrNoSession is returned when no session cookie can be found. The message is
// the whole of the setup this feature needs, so it is written out in full
// rather than left as "unauthorized".
var ErrNoSession = errors.New(`no Advent of Code session cookie

Your puzzle input is yours: the site hands it out against the session cookie
your browser holds. To let this fetch it, copy that cookie once:

  1. log in at https://adventofcode.com in a browser
  2. open the developer tools, Application (or Storage) → Cookies
  3. copy the value of the cookie named "session"

then either set it in the environment:

  export AOC_SESSION=<the value>

or write it to a file this looks for:

  ~/.config/domain/aoc-session

The cookie is a login. Keep it out of anything you commit, and note that it
expires after about a month — a 400 from the site usually means it has.`)

// NewClient builds a client from the environment: the session cookie, the
// cache directory, and the contact address the guidelines ask automation to
// carry.
func NewClient() (*Client, error) {
	session, err := findSession()
	if err != nil {
		return nil, err
	}
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}
	return &Client{
		Session:  session,
		BaseURL:  site,
		CacheDir: dir,
		Contact:  os.Getenv("AOC_CONTACT"),
		HTTP:     &http.Client{Timeout: 30 * time.Second},
		Now:      time.Now,
	}, nil
}

// findSession looks where a session cookie is kept, in the order someone is
// likely to have put one.
//
// The environment first, because that is what a shell profile or a `.envrc`
// sets and it is the one a person can see. Then this tool's own config file.
// Then `~/.adventofcode.session`, which several other AoC helpers use — if one
// is already there, asking for the same cookie a second time under a different
// name would be a poor greeting.
func findSession() (string, error) {
	if s := cleanSession(os.Getenv("AOC_SESSION")); s != "" {
		return s, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ErrNoSession
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	for _, path := range []string{
		filepath.Join(config, "domain", "aoc-session"),
		filepath.Join(home, ".adventofcode.session"),
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if s := cleanSession(string(b)); s != "" {
			return s, nil
		}
	}
	return "", ErrNoSession
}

// cleanSession takes what a person pasted and returns the cookie's value.
//
// The two mistakes worth absorbing are pasting the whole `session=abc…` pair
// out of a cookie inspector, and the trailing newline every editor adds to a
// file. Neither is worth a 400 from the server and an afternoon of wondering.
func cleanSession(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "session=")
	if i := strings.IndexAny(s, "; \t\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// cacheDir is where fetched pages and inputs live: under the user's cache
// directory, because that is what it is — everything in it can be fetched
// again, and nothing in it is the user's own work.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory to keep puzzles in: %w", err)
	}
	return filepath.Join(base, "domain", "aoc"), nil
}

// userAgent identifies this tool to the server.
//
// The guidelines ask automation to say what it is and how to reach whoever is
// running it. The first half is knowable here; the second is not, so
// `AOC_CONTACT` supplies it and its absence is stated rather than faked.
func (c *Client) userAgent() string {
	ua := "domain-expansion-development (+https://github.com/RFuller25/domain)"
	if contact := strings.TrimSpace(c.Contact); contact != "" {
		return ua + " by " + contact
	}
	return ua
}

// clock reads the client's clock, defaulting to the real one.
func (c *Client) clock() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// get fetches one URL with the session cookie on it.
func (c *Client) get(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c.decorate(req)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp.StatusCode, string(body))
	}
	return string(body), nil
}

// decorate puts the cookie and the identification on a request. Both go on
// every request, which is why they are here rather than at each call site.
func (c *Client) decorate(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: "session", Value: c.Session})
	req.Header.Set("User-Agent", c.userAgent())
}

// statusError turns a non-200 into something that says what to do about it.
//
// The site answers an expired or missing cookie with a 400 and a page of prose
// rather than a 401, so the status alone would send a reader looking for the
// wrong problem.
func statusError(code int, body string) error {
	switch {
	case code == http.StatusBadRequest && strings.Contains(body, "log in"):
		return errors.New("the session cookie was refused — it has probably expired (they last about a month); copy a fresh one")
	case code == http.StatusNotFound:
		return errors.New("no such puzzle — the day is not out yet, or the year and day are not a puzzle")
	}
	return fmt.Errorf("adventofcode.com answered %d", code)
}

// ---------------------------------------------------------------------------
// what may be asked for
// ---------------------------------------------------------------------------

// FirstYear is the first Advent of Code.
const FirstYear = 2015

// Validate reports whether a year and day are a puzzle that exists yet.
//
// This is a courtesy to the server and to the reader in equal measure: a
// request for day 30, or for next December, is a request that can only be
// refused, and refusing it here says why in terms of the calendar rather than
// as a 404.
func Validate(year, day int, now time.Time) error {
	if day < 1 || day > 25 {
		return fmt.Errorf("day %d — Advent of Code runs from day 1 to day 25", day)
	}
	if year < FirstYear {
		return fmt.Errorf("year %d — the first Advent of Code was %d", year, FirstYear)
	}
	latest := LatestYear(now)
	if year > latest {
		return fmt.Errorf("year %d has not happened yet — the most recent is %d", year, latest)
	}
	if unlock := Unlock(year, day); now.Before(unlock) {
		return fmt.Errorf("%d day %d unlocks %s (in %s)",
			year, day, unlock.Local().Format("Mon 2 Jan 15:04"), until(unlock.Sub(now)))
	}
	return nil
}

// LatestYear is the most recent year with puzzles in it. Before December a
// year has none, which is the difference between this and now.Year().
func LatestYear(now time.Time) int {
	y := now.UTC().Year()
	if now.UTC().Month() < time.December {
		y--
	}
	return y
}

// Unlock is when a day's puzzle appears: midnight EST, which is UTC-5 and does
// not move, since December is outside daylight saving everywhere that matters
// to this.
func Unlock(year, day int) time.Time {
	return time.Date(year, time.December, day, 5, 0, 0, 0, time.UTC)
}

// until renders a duration the way someone waiting for one would read it.
func until(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 1))
	}
}

// ---------------------------------------------------------------------------
// fetching
// ---------------------------------------------------------------------------

// Fetch returns one day's puzzle and input, from the cache when they are
// there.
//
// The two halves are cached separately because they age differently: an input
// is fixed forever the moment it is generated, while the puzzle page grows a
// second half when the first is solved. Refresh re-reads the page; nothing
// ever re-reads the input.
func (c *Client) Fetch(year, day int) (*Puzzle, error) {
	return c.fetch(year, day, false)
}

// Refresh fetches the puzzle page again, keeping the cached input.
//
// This is what solving part one is followed by: the page that describes part
// two did not exist when the page in the cache was read.
func (c *Client) Refresh(year, day int) (*Puzzle, error) {
	return c.fetch(year, day, true)
}

func (c *Client) fetch(year, day int, refresh bool) (*Puzzle, error) {
	if err := Validate(year, day, c.clock()); err != nil {
		return nil, err
	}
	page, err := c.page(year, day, refresh)
	if err != nil {
		return nil, err
	}
	input, err := c.input(year, day)
	if err != nil {
		return nil, err
	}
	p := parsePuzzle(year, day, page)
	p.Input = input
	return p, nil
}

// page is the puzzle's HTML, cached.
func (c *Client) page(year, day int, refresh bool) (string, error) {
	path := c.cachePath(year, day, "puzzle.html")
	if !refresh {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b), nil
		}
	}
	body, err := c.get(fmt.Sprintf("%s/%d/day/%d", c.BaseURL, year, day))
	if err != nil {
		return "", err
	}
	if err := writeCache(path, body); err != nil {
		return "", err
	}
	return body, nil
}

// input is the personal puzzle input, cached forever.
//
// Forever is the point. The file the server hands back for a given day and
// session never changes, so having fetched it once, asking again is pure cost
// to somebody else's machine — which is exactly what the automation guidelines
// ask us not to do.
func (c *Client) input(year, day int) (string, error) {
	path := c.cachePath(year, day, "input.txt")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b), nil
	}
	body, err := c.get(fmt.Sprintf("%s/%d/day/%d/input", c.BaseURL, year, day))
	if err != nil {
		return "", err
	}
	if err := writeCache(path, body); err != nil {
		return "", err
	}
	return body, nil
}

// Cached reports whether a day is already on disk in full, so a caller can say
// "reading" rather than "fetching" — and so it can tell whether a network is
// about to be needed at all.
func (c *Client) Cached(year, day int) bool {
	for _, name := range []string{"puzzle.html", "input.txt"} {
		if st, err := os.Stat(c.cachePath(year, day, name)); err != nil || st.Size() == 0 {
			return false
		}
	}
	return true
}

// cachePath is where one of a day's files lives.
func (c *Client) cachePath(year, day int, name string) string {
	return filepath.Join(c.CacheDir, fmt.Sprintf("%d", year), fmt.Sprintf("%02d", day), name)
}

// writeCache saves a fetched file, creating the day's directory.
//
// The input is personal — the site asks that inputs not be shared, and a
// cookie's worth of someone's account went into fetching it — so the files are
// written 0600 and the directories 0700 rather than at the usual defaults.
func writeCache(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}
