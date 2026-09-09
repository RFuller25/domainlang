package game

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"domain/ir"
)

const wordleGame = `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {guess: "crane", marks: "", note: "", out: 1 = 0}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/guess/" + w.guess
        Method: "POST"
        Body: (w) -> tojson({word: w.guess})
        As: "guess"
        Into: {accepted: Bool, marks: List<Text>}
    Cursed Technique: Apply
        Using: (w) -> with(w, "out", 1 = 1)

Part Reply "guess":
    Cursed Technique: Apply
        Using: (w) -> if reply.ok
            then with(with(w, "marks", textjoin(reply.value.marks, "")), "out", 1 = 0)
            else with(with(w, "note", reply.error), "out", 1 = 0)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.guess + "|" + w.marks + "|" + w.note)
`

// The whole asynchronous exchange, with no server anywhere: a replayed run
// never touches the network, and the script says what came back. That is what
// makes a multiplayer game testable at all — the two halves become two lines
// of a file — and it is why the examples need nothing running.
func TestReplyArrivesFromTheScript(t *testing.T) {
	got := replay(t, wordleGame,
		"key enter\nframe\nreply guess {\"accepted\": true, \"marks\": [\"G\",\"Y\",\"-\"]}\nframe\nquit\n")
	want := "crane||\n\ncrane|GY-|"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// Every way a request can go wrong lands in the same record, so a game has one
// thing to branch on and "the server is down" is a state it can draw.
func TestFailedReplyCarriesTheReason(t *testing.T) {
	got := replay(t, wordleGame, "key enter\nfail guess connection refused\nframe\nquit\n")
	if want := "crane||connection refused"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A request the script never answers simply never arrives. That is itself a
// state worth testing: it is what a server going away mid-game looks like, and
// there is no other way to write a test for it.
func TestAnUnansweredRequestNeverArrives(t *testing.T) {
	if got, want := replay(t, wordleGame, "key enter\nframe\nquit\n"), "crane||"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// pending() is how the one thing a bounded tag costs — a dropped reply — is
// made visible, so a program that must not lose one can guard.
func TestPendingTracksARequest(t *testing.T) {
	src := strings.Replace(wordleGame,
		`Using: (w) -> text(w.guess + "|" + w.marks + "|" + w.note)`,
		`Using: (w) -> text(if pending("guess") then "out" else "idle")`, 1)
	got := replay(t, src,
		"frame\nkey enter\nframe\nreply guess {\"accepted\": true, \"marks\": []}\nframe\nquit\n")
	if want := "idle\n\nout\n\nidle"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplayReplyErrors(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{"reply\n", "needs a tag"},
		{"reply nosuch {}\n", `no ` + "`" + `Part Reply "nosuch":` + "`"},
		{"reply guess notjson\n", "does not fit"},
		{"reply guess {\"accepted\": true}\n", "does not fit"},
	} {
		if _, err := runReplay(wordleGame, c.script); err == nil {
			t.Errorf("script %q was accepted", c.script)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("script %q: error = %v, want it to mention %q", c.script, err, c.want)
		}
	}
}

// The transport itself, against a real server. Everything above proves the
// language and the host; this proves the wire — that a body goes out, a status
// comes back, and a document is decoded into the declared shape.
func TestSendRequestAgainstAServer(t *testing.T) {
	var gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"score": 7}`))
		case "/teapot":
			w.WriteHeader(http.StatusTeapot)
		default:
			_, _ = w.Write([]byte(`not json`))
		}
	}))
	defer srv.Close()

	into := ir.Record(ir.Field{Name: "score", Type: ir.Int()})
	spec := &ir.RequestSpec{Tag: "t", Method: "POST", Into: into, Reply: ir.ReplyType(into), TimeoutMS: 2000}
	client := httpClient(2000)

	t.Run("a good answer decodes", func(t *testing.T) {
		got := sendRequest(client, ir.RequestCall{Spec: spec, URL: srv.URL + "/ok",
			Body: `{"word":"crane"}`, HasBody: true}, 1)
		if got.err != "" {
			t.Fatalf("unexpected failure: %s", got.err)
		}
		if gotMethod != "POST" || gotBody != `{"word":"crane"}` {
			t.Errorf("the server saw %s %q", gotMethod, gotBody)
		}
		if s := ir.FormatValue(got.value); !strings.Contains(s, "score: 7") {
			t.Errorf("value = %s", s)
		}
	})

	t.Run("a bad status is a failure, not a decode", func(t *testing.T) {
		got := sendRequest(client, ir.RequestCall{Spec: spec, URL: srv.URL + "/teapot"}, 1)
		if !strings.Contains(got.err, "418") {
			t.Errorf("error = %q, want it to name the status", got.err)
		}
	})

	t.Run("an undecodable body says so", func(t *testing.T) {
		got := sendRequest(client, ir.RequestCall{Spec: spec, URL: srv.URL + "/junk"}, 1)
		if !strings.Contains(got.err, "not valid JSON") {
			t.Errorf("error = %q", got.err)
		}
	})

	t.Run("a failure still carries a value of the declared shape", func(t *testing.T) {
		got := sendRequest(client, ir.RequestCall{Spec: spec, URL: "http://127.0.0.1:1/nope"}, 1)
		if got.err == "" {
			t.Fatal("expected a failure")
		}
		r, ok := got.value.(*ir.RecordValue)
		if !ok {
			t.Fatalf("value = %s, want a reply record", ir.DescribeValue(got.value))
		}
		if v, _ := r.Get("ok"); v != false {
			t.Errorf("ok = %v, want false", v)
		}
		inner, _ := r.Get("value")
		if s := ir.FormatValue(inner); !strings.Contains(s, "score: 0") {
			t.Errorf("the zero value is %s, want a record of zeroes", s)
		}
	})
}

// One request is in flight per tag, and firing again supersedes it: the answer
// to a superseded firing arrives late and is dropped. Out-of-order replies are
// what makes this a correctness rule rather than a preference — a stale
// position merged after a fresh one makes other players teleport backwards.
func TestOneRequestInFlightPerTag(t *testing.T) {
	box := newRequestBox()
	first := box.begin("pos")
	if !box.pending("pos") {
		t.Error("a fired request should be pending")
	}
	second := box.begin("pos")
	if box.accept("pos", first) {
		t.Error("the superseded firing's answer was accepted")
	}
	if !box.accept("pos", second) {
		t.Error("the live firing's answer was refused")
	}
	if box.pending("pos") {
		t.Error("nothing should be pending once the answer arrived")
	}
}
