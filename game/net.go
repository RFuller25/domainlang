// Sending a request, and delivering the answer.
//
// One request is in flight per tag, and firing again supersedes the one that
// was out. That is a correctness rule before it is a policy: **HTTP replies
// can arrive out of order**, and a client polling positions at 5 Hz against a
// server that sometimes takes half a second will, under any unbounded scheme,
// receive an answer from three requests ago *after* a newer one and merge it.
// The symptom is other players teleporting backwards, which reads as a game
// bug rather than a networking one and costs a day to find. Bounding the tag
// makes that interleaving unrepresentable rather than unlikely, and caps the
// backlog a slow server can build.
//
// It supersedes rather than dropping the new one because a request body is
// normally derived from the current world: superseding sends where the player
// *is*, keeping the older one sends where they *were*.
//
// The one thing it costs — a dropped reply — is visible. `pending("tag")` says
// whether one is out, so a program that must not lose one guards first.
package game

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"domain/ir"
)

// maxReplyBytes caps a response. A game holds its whole world in memory and
// paints it sixty times a second; a server answering with a gigabyte would
// take it down, and no answer a game asks for is that large.
const maxReplyBytes = 8 << 20

// replyMsg is one completed request, on its way back to the program.
type replyMsg struct {
	tag string
	// seq identifies which firing this answers. A superseded request may
	// still be in the air when its replacement is fired, and its answer must
	// be dropped rather than delivered late.
	seq   int64
	value ir.Value
	err   string
}

// requestBox tracks what is out, per tag.
type requestBox struct {
	mu   sync.Mutex
	seq  map[string]int64
	live map[string]bool
}

func newRequestBox() *requestBox {
	return &requestBox{seq: map[string]int64{}, live: map[string]bool{}}
}

// begin records a firing and returns its sequence number, superseding
// whatever was out under that tag.
func (b *requestBox) begin(tag string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq[tag]++
	b.live[tag] = true
	return b.seq[tag]
}

// accept reports whether a reply is the one still being waited for. A
// superseded firing's answer is dropped here.
func (b *requestBox) accept(tag string, seq int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seq[tag] != seq {
		return false
	}
	b.live[tag] = false
	return true
}

func (b *requestBox) pending(tag string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live[tag]
}

// sendRequest performs one request and turns everything that can go wrong into the
// same shape: `{ok: false, error: …, value: <zero>}`. A game has one thing to
// branch on, and "the server is down" becomes a state it can draw rather than
// a crash mid-frame.
func sendRequest(client *http.Client, call ir.RequestCall, seq int64) replyMsg {
	spec := call.Spec
	out := replyMsg{tag: spec.Tag, seq: seq}
	fail := func(format string, a ...any) replyMsg {
		out.err = fmt.Sprintf(format, a...)
		out.value = ir.Reply(spec.Into, nil, out.err)
		return out
	}

	var body io.Reader
	if call.HasBody {
		body = strings.NewReader(call.Body)
	}
	req, err := http.NewRequest(spec.Method, call.URL, body)
	if err != nil {
		return fail("%v", err)
	}
	if call.HasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail("%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fail("the server answered %s", resp.Status)
	}
	text, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes+1))
	if err != nil {
		return fail("%v", err)
	}
	if len(text) > maxReplyBytes {
		return fail("the answer is larger than %d bytes", maxReplyBytes)
	}
	v, err := ir.FromJSON(string(text), spec.Into)
	if err != nil {
		return fail("%v", err)
	}
	out.value = ir.Reply(spec.Into, v, "")
	return out
}

// httpClient is the client a played game uses. One per run, with the timeout
// the request declared applied per call.
func httpClient(timeoutMS int64) *http.Client {
	return &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
}
