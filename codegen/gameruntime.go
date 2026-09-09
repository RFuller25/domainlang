// The runtime a compiled game carries, mirroring game/*.go.
//
// codegen generates its own runtime rather than importing this repository's
// (see codegen/viewgen.go), so the loop that drives a game exists twice: once
// as the interpreter's host, once here. The two must agree exactly, because a
// replayed frame is the oracle — codegen/game_test.go diffs one against the
// other for every anchor game, in both optimizer modes, and a divergence in
// this file is what that test exists to find.
//
// Everything below is a transliteration, and the comments on the interpreter's
// copy are the reasons. What is repeated here is only what a reader of the
// generated program needs in order to trust it.
package codegen

// declGameState is the world's neighbours: what a run carries that no Part is
// a function of. The world itself is generated (its type comes from the
// program), so it is declared in gamegen.go rather than here.
const declGameState = `var dmQuit bool
var dmBeep bool
// dmLoadScripted and dmHasLoadScripted are a replayed run's stand-in for a
// real file: set by a 'load' script line (dmCommand below), read by dmLoad
// in place of the disk a replayed run must never touch.
var dmLoadScripted string
var dmHasLoadScripted bool
var dmCols int64 = 40
var dmRows int64 = 20
var dmNowMS int64
var dmDueMS []int64

// dmCall is one firing of a Request: the spec, plus what the lambdas made of
// the world.
type dmCall struct {
	tag, method, url, body string
	hasBody                bool
	timeoutMS              int64
	seq                    int64
}

// dmFired is what the body that just ran asked for. A Request cannot hand a
// command back — it does not know it is inside one — so it queues here and
// whoever was driving turns it into one.
var dmFired []dmCall

func dmFire(c dmCall) {
	c.seq = dmBox.begin(c.tag)
	dmFired = append(dmFired, c)
}

// dmBox bounds requests to one in flight per tag, superseding rather than
// queueing. HTTP replies can arrive out of order, and an unbounded scheme
// merges a stale position after a fresh one; see game/net.go.
type dmBoxT struct {
	mu   sync.Mutex
	seq  map[string]int64
	live map[string]bool
}

var dmBox = &dmBoxT{seq: map[string]int64{}, live: map[string]bool{}}

func (b *dmBoxT) begin(tag string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq[tag]++
	b.live[tag] = true
	return b.seq[tag]
}

func (b *dmBoxT) accept(tag string, seq int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seq[tag] != seq {
		return false
	}
	b.live[tag] = false
	return true
}

func (b *dmBoxT) clear(tag string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.live[tag] = false
}

func dmPending(tag string) bool {
	dmBox.mu.Lock()
	defer dmBox.mu.Unlock()
	return dmBox.live[tag]
}

// dmLoad answers a Load: the raw JSON text a save file held, or a
// 'load' script line stood in for one, and whether there was one at all.
// A replayed run never opens a real file, on the same terms it never
// sends a real request.
func dmLoad(path string) (string, bool) {
	if !dmPlayable() {
		return dmLoadScripted, dmHasLoadScripted
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// dmSave writes state that outlives the run. A replayed run skips it
// entirely; a played run that fails to write has nowhere to carry the
// failure back to — there is no Reply, unlike a Request — so it is noted
// on stderr and otherwise swallowed.
func dmSave(path, json string) {
	if !dmPlayable() {
		return
	}
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "save %s: %v\n", path, err)
	}
}`

// declGameEvents mirrors prims/gameevent.go: which handlers answer to a piece
// of input, and in what order. The specs are a closed table in the language,
// so this is a transliteration of the language's own rule rather than a second
// opinion about it.
const declGameEvents = `func dmEventMatch(spec, kind, name string) bool {
	switch {
	case spec == "resize":
		return kind == "resize"
	case spec == "text":
		return kind == "text"
	case spec == "key":
		return kind == "key"
	case strings.HasPrefix(spec, "key "):
		return kind == "key" && strings.TrimSpace(strings.TrimPrefix(spec, "key ")) == name
	}
	return false
}

// dmEventPriority: a named key runs before the catch-all, so a program that
// writes both "key q" and "key" sees the specific one first.
func dmEventPriority(spec string) int {
	if strings.HasPrefix(spec, "key ") {
		return 2
	}
	return 1
}

// dmHandlers is the indices of the Part On blocks that answer to an event, in
// the order they run: specific first, otherwise as written.
func dmHandlers(kind, name string) []int {
	var out []int
	for i, spec := range dmOnSpecs {
		if dmEventMatch(spec, kind, name) {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return dmEventPriority(dmOnSpecs[out[a]]) > dmEventPriority(dmOnSpecs[out[b]])
	})
	return out
}

// dmEvent delivers one thing that happened to every handler that answers to it.
func dmEvent(kind, name string, w, h int64) {
	for _, i := range dmHandlers(kind, name) {
		dmOnRun(i, name, w, h)
		if dmQuit {
			return
		}
	}
}`

// declGameReplay is the oracle: events from a script, frames to stdout.
//
// Deterministic because three decisions made it so — a virtual clock advanced
// only by the script, globals that freeze once the program starts, and a seed
// that is a line of the script rather than the time of day.
const declGameReplay = `var dmFrameCount int

func dmReplayRun() {
	dmSeed(1)
	dmDueMS = make([]int64, len(dmTimerPeriods))
	copy(dmDueMS, dmTimerPeriods)
	dmBegin()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan() && !dmQuit; line++ {
		text := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		if err := dmCommand(text); err != nil {
			dmFail("replay script line %d: %v", line, err)
		}
	}
	if err := sc.Err(); err != nil {
		dmFail("reading the replay script: %v", err)
	}
	dmFinish()
}

// dmFrame draws one frame. Frames are separated by a blank line rather than by
// a rule, so a one-frame program prints its picture and nothing else.
func dmFrame() {
	v := dmDrawView()
	dmFrames++
	if dmFrameCount > 0 {
		fmt.Fprintln(os.Stdout)
	}
	if dmBeep {
		// The bell character itself, with no newline of its own: it rings
		// without displacing a line of the picture.
		fmt.Fprint(os.Stdout, "\a")
		dmBeep = false
	}
	fmt.Fprintln(os.Stdout, dmViewPlain(v))
	dmFrameCount++
}

// dmAdvance moves the virtual clock forward by ms, firing every timer that
// falls due on the way, in due order.
func dmAdvance(ms int64) {
	target := dmNowMS + ms
	for !dmQuit {
		next, idx := int64(-1), -1
		for i, due := range dmDueMS {
			if due <= target && (next < 0 || due < next) {
				next, idx = due, i
			}
		}
		if idx < 0 {
			break
		}
		dmElapsedMS += next - dmNowMS
		dmNowMS = next
		dmDueMS[idx] = next + dmTimerPeriods[idx]
		dmTimerRun(idx)
	}
	dmElapsedMS += target - dmNowMS
	dmNowMS = target
}

// dmTick advances to the next timer that is due, which is what a bare tick
// means: let the next thing happen.
func dmTick() {
	if len(dmDueMS) == 0 {
		return
	}
	next := dmDueMS[0]
	for _, due := range dmDueMS {
		if due < next {
			next = due
		}
	}
	dmAdvance(next - dmNowMS)
}

func dmCommand(text string) error {
	verb, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	switch verb {
	case "frame":
		dmFrame()
		return nil
	case "quit":
		dmQuit = true
		return nil
	case "key":
		if rest == "" {
			return fmt.Errorf("` + "`key`" + ` needs a key name, e.g. ` + "`key up`" + `")
		}
		dmEvent("key", rest, 0, 0)
		return nil
	case "text":
		if rest == "" {
			return fmt.Errorf("` + "`text`" + ` needs something typed, e.g. ` + "`text a`" + `")
		}
		dmEvent("text", rest, 0, 0)
		return nil
	case "tick":
		if rest == "" {
			dmTick()
			return nil
		}
		ms, err := strconv.ParseInt(rest, 10, 64)
		if err != nil || ms < 0 {
			return fmt.Errorf("` + "`tick`" + ` takes a number of milliseconds, got %q", rest)
		}
		dmAdvance(ms)
		return nil
	case "reply", "fail":
		return dmScriptReply(verb == "reply", rest)
	case "seed":
		n, err := strconv.ParseUint(rest, 10, 64)
		if err != nil {
			return fmt.Errorf("` + "`seed`" + ` takes a number, got %q", rest)
		}
		dmSeed(n)
		return nil
	case "load":
		if rest == "" {
			return fmt.Errorf("` + "`load`" + ` needs a document, e.g. ` + "`load {}`" + `")
		}
		dmLoadScripted, dmHasLoadScripted = rest, true
		return nil
	case "size":
		w, h, err := dmParseSize(rest)
		if err != nil {
			return err
		}
		dmCols, dmRows = w, h
		dmEvent("resize", "", w, h)
		return nil
	}
	return fmt.Errorf("unknown replay command %q; the commands are: "+
		"frame, key <name>, text <what>, tick [ms], size <w> <h>, seed <n>, "+
		"reply <tag> <json>, fail <tag> <why>, load <json>, quit", verb)
}

func dmParseSize(rest string) (int64, int64, error) {
	f := strings.Fields(rest)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("` + "`size`" + ` takes a width and a height, e.g. ` + "`size 40 20`" + `")
	}
	w, err1 := strconv.ParseInt(f[0], 10, 64)
	h, err2 := strconv.ParseInt(f[1], 10, 64)
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("` + "`size`" + ` takes two positive numbers, got %q", rest)
	}
	return w, h, nil
}

// dmScriptReply answers a request the program fired, from the script rather
// than from a server. A request the script never answers simply never arrives,
// which is itself a state worth testing.
func dmScriptReply(ok bool, rest string) error {
	tag, body, _ := strings.Cut(rest, " ")
	tag, body = strings.TrimSpace(tag), strings.TrimSpace(body)
	if tag == "" {
		return fmt.Errorf("` + "`reply`" + ` needs a tag, e.g. ` + "`reply scores {\\\"top\\\": 3}`" + `")
	}
	if !dmHasReply(tag) {
		return fmt.Errorf("no ` + "`Part Reply %q:`" + ` in this program", tag)
	}
	if !ok && body == "" {
		body = "the request failed"
	}
	dmBox.clear(tag)
	return dmReplyDeliver(tag, ok, body)
}`

// declGameNet is the sending half of a request. Everything that can go wrong
// becomes the same shape — {ok: false, error: …, value: <zero>} — so a game has
// one thing to branch on and "the server is down" is a state it can draw.
const declGameNet = `const dmMaxReplyBytes = 8 << 20

type dmReplyMsg struct {
	tag  string
	seq  int64
	ok   bool
	text string
}

func dmSend(client *http.Client, c dmCall) dmReplyMsg {
	out := dmReplyMsg{tag: c.tag, seq: c.seq}
	fail := func(format string, a ...any) dmReplyMsg {
		out.ok, out.text = false, fmt.Sprintf(format, a...)
		return out
	}
	var body io.Reader
	if c.hasBody {
		body = strings.NewReader(c.body)
	}
	req, err := http.NewRequest(c.method, c.url, body)
	if err != nil {
		return fail("%v", err)
	}
	if c.hasBody {
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
	text, err := io.ReadAll(io.LimitReader(resp.Body, dmMaxReplyBytes+1))
	if err != nil {
		return fail("%v", err)
	}
	if len(text) > dmMaxReplyBytes {
		return fail("the answer is larger than %d bytes", dmMaxReplyBytes)
	}
	out.ok, out.text = true, string(text)
	return out
}`

// declGamePlay is the interactive half: events from the terminal, frames
// painted on it. It differs from the replayed half only at its edges — the
// clock is real, and Draw runs after every message because Bubble Tea calls
// View after every Update.
const declGamePlay = `type dmTickMsg struct{ idx int }

type dmModel struct{}

// dmPlayable reports whether this run has a terminal to play on. It is the
// same question Read Source asks about a file before falling back to stdin,
// and it is asked for the same reason: the program should do the obvious
// thing without being told which mode to use.
func dmPlayable() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

func dmPlay() {
	// Played, the stream starts from the clock, so a game is a different game
	// every time somebody sits down to it.
	dmSeed(uint64(time.Now().UnixNano()))
	dmBegin()
	if _, err := tea.NewProgram(dmModel{}).Run(); err != nil {
		dmFail("%v", err)
	}
	// Ending runs after the loop, outside the alternate screen, so its Reveal
	// lands in the scrollback the player gets back.
	dmFinish()
}

func (m dmModel) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(dmTimerPeriods)+1)
	for i, period := range dmTimerPeriods {
		cmds = append(cmds, dmTimerCmd(i, period))
	}
	// Part World and Part Start ran before the loop began, and either may have
	// fired a request. Those were queued with nothing to turn them into
	// commands, so this is where they go out.
	if c := dmDrain(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func dmTimerCmd(idx int, periodMS int64) tea.Cmd {
	d := time.Duration(periodMS) * time.Millisecond
	return tea.Tick(d, func(time.Time) tea.Msg { return dmTickMsg{idx: idx} })
}

func (m dmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if dmQuit {
		return m, tea.Quit
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		dmCols, dmRows = int64(msg.Width), int64(msg.Height)
		dmEvent("resize", "", dmCols, dmRows)
	case dmTickMsg:
		if msg.idx < len(dmTimerPeriods) {
			dmElapsedMS += dmTimerPeriods[msg.idx]
			dmTimerRun(msg.idx)
			if dmQuit {
				return m, tea.Quit
			}
			return m, tea.Batch(dmDrain(), dmTimerCmd(msg.idx, dmTimerPeriods[msg.idx]))
		}
	case dmReplyMsg:
		// A superseded firing's answer arrives late and is dropped: the
		// program asked for newer data and already has a newer question out.
		if !dmBox.accept(msg.tag, msg.seq) || !dmHasReply(msg.tag) {
			return m, nil
		}
		if err := dmReplyDeliver(msg.tag, msg.ok, msg.text); err != nil {
			// A body that does not decode is a failure like any other: the
			// program sees {ok: false} and draws it.
			_ = dmReplyDeliver(msg.tag, false, err.Error())
		}
	case tea.KeyPressMsg:
		name := msg.String()
		// Ctrl+C always leaves, unless the program said what it means by it.
		if name == "ctrl+c" && len(dmHandlers("key", name)) == 0 {
			return m, tea.Quit
		}
		dmEvent("key", name, 0, 0)
		// A printable keystroke is also text, which is what a game asking the
		// player to type reads. Both handlers see it.
		if !dmQuit && msg.Text != "" {
			dmEvent("text", msg.Text, 0, 0)
		}
	}
	if dmQuit {
		return m, tea.Quit
	}
	return m, dmDrain()
}

// dmDrain turns everything the bodies just fired into commands.
func dmDrain() tea.Cmd {
	if len(dmFired) == 0 {
		return nil
	}
	calls := dmFired
	dmFired = nil
	cmds := make([]tea.Cmd, 0, len(calls))
	for _, c := range calls {
		// The timeout the request declared, applied per call.
		client := &http.Client{Timeout: time.Duration(c.timeoutMS) * time.Millisecond}
		cmds = append(cmds, func() tea.Msg { return dmSend(client, c) })
	}
	return tea.Batch(cmds...)
}

func (m dmModel) View() tea.View {
	v := dmDrawView()
	dmFrames++
	s := dmViewANSI(v)
	if dmBeep {
		s = "\a" + s
		dmBeep = false
	}
	out := tea.NewView(s)
	out.AltScreen = true
	return out
}`

// declGameANSI paints a laid-out View. The geometry is already decided —
// dmViewLayout reduced the tree to a rectangle of styled runs — and this only
// chooses how each run is drawn. That split is why a replayed frame and a
// played one are the same picture, and there must never be any layout here.
//
// The codes are written directly rather than through a styling library: they
// are the sixteen ANSI colours a program may name, and an emitted program that
// can paint them needs nothing that knows about colour profiles.
const declGameANSI = `var dmANSIColour = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"bright-black": 8, "bright-red": 9, "bright-green": 10, "bright-yellow": 11,
	"bright-blue": 12, "bright-magenta": 13, "bright-cyan": 14, "bright-white": 15,
}

func dmANSIBase(idx, base int) int {
	if idx < 8 {
		return base + idx
	}
	return base + 60 + idx - 8
}

func dmStyleRun(text string, st dmViewStyle) string {
	var codes []string
	if st.bold {
		codes = append(codes, "1")
	}
	if st.dim {
		codes = append(codes, "2")
	}
	if st.underline {
		codes = append(codes, "4")
	}
	if st.reverse {
		codes = append(codes, "7")
	}
	if idx, ok := dmANSIColour[st.fg]; ok {
		codes = append(codes, strconv.Itoa(dmANSIBase(idx, 30)))
	}
	if idx, ok := dmANSIColour[st.bg]; ok {
		codes = append(codes, strconv.Itoa(dmANSIBase(idx, 40)))
	}
	if len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

func dmViewANSI(v dmView) string {
	b := dmViewLayout(v, dmViewStyle{})
	lines := make([]string, len(b.lines))
	for i, line := range b.lines {
		var sb strings.Builder
		for _, run := range line {
			sb.WriteString(dmStyleRun(run.text, run.style))
		}
		// Trailing spaces are trimmed for the same reason the plain renderer
		// trims them: they are invisible, and on a terminal they also paint a
		// background colour across the rest of the row.
		lines[i] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}`
