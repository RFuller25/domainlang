package ir

// Asking a server something, without waiting for the answer.
//
// A game cannot block on a round trip: a hundred milliseconds is six frames,
// and a frozen picture every time the program asks a question is not a game.
// So a request is *fired*, the world carries on, and the answer arrives later
// as an event like any other.
//
// These types are the contract between the three parts of that: the resolver,
// which knows what the program declared; the host, which does the sending; and
// the reply Part, which is typed against what comes back.

// RequestSpec is what a `Domain Expansion: Request` statement declared.
type RequestSpec struct {
	Tag       string // the `As:` name; a `Part Reply "<tag>":` answers to it
	Method    string // GET by default
	Into      *Type  // the shape the body is decoded into
	Reply     *Type  // {ok: Bool, error: Text, value: Into}
	TimeoutMS int64
}

// CollectRequestSpecs walks a pipeline for the requests it can fire, keyed by
// tag.
//
// A Request may sit anywhere a statement may — inside a Start, an input
// handler, a timer, a Shikigami inlined into any of them — so this recurses
// through every node list rather than looking only at the top level. Missing
// one would mean a reply arriving with no shape to decode into.
func CollectRequestSpecs(nodes []*Node, out map[string]*RequestSpec) {
	for _, n := range nodes {
		if n == nil || n.Meta == nil {
			continue
		}
		if spec, ok := n.Meta["request"].(*RequestSpec); ok && spec != nil {
			out[spec.Tag] = spec
		}
		if sub, ok := n.Meta["nodes"].([]*Node); ok {
			CollectRequestSpecs(sub, out)
		}
		if subs, ok := n.Meta[MetaBindNodes].([][]*Node); ok {
			for _, s := range subs {
				CollectRequestSpecs(s, out)
			}
		}
	}
}

// RequestCall is one firing: the spec, plus what the lambdas made of the world.
type RequestCall struct {
	Spec    *RequestSpec
	URL     string
	Body    string
	HasBody bool
}

// ReplyType is the shape a reply always has, whatever was asked for.
//
// Every failure lands in it: a server that is not there, a status that is not
// 2xx, a timeout, a body that does not decode. One shape means a game has one
// thing to branch on, and "the server is down" becomes a state it can draw
// rather than a crash — which for a game that is mid-frame is the difference
// between "reconnecting…" and a stack trace over a half-painted screen.
//
// When ok is false, value is the zero of what was asked for (ZeroValue). There
// is no optional type in this language and inventing one for this would be a
// large change to serve a small case; a harmless value the program has no
// reason to read is the smaller answer.
func ReplyType(into *Type) *Type {
	return Record(
		Field{Name: "ok", Type: Bool()},
		Field{Name: "error", Type: Text()},
		Field{Name: "value", Type: into},
	)
}

// Reply builds the value a `Part Reply` body receives.
func Reply(into *Type, value Value, err string) Value {
	r := NewRecordValueSized(3)
	r.Set("ok", err == "")
	r.Set("error", err)
	if err == "" {
		r.Set("value", value)
	} else {
		r.Set("value", ZeroValue(into))
	}
	return r
}
