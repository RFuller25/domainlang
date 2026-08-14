// Package ast defines the parsed tree for Domain. It distinguishes the two
// syntactic layers: the themed pipeline layer (Statement / Operation) and the
// plain expression layer (Expr / Lambda), which appears inside Using: lambdas
// and Binding Vow predicates.
package ast

import (
	"slices"
	"strings"

	"domain/token"
)

// Keywords is the canonical list of themed statement keywords. Writing one is
// optional — a statement may be written as a bare operation phrase and
// prims.Infer recovers the keyword from the phrase — but a keyword that *is*
// written must be spelled from this list.
//
// The list lives here, in the syntax layer, because the parser needs it to
// tell `Reveal stdout` (a keyword missing its colon) apart from a prefix-free
// phrase. A test in package prims pins it to the keywords the primitive
// registry actually uses, so the two cannot drift.
var Keywords = []string{
	"Innate Domain",
	"Cursed Energy",
	"Cursed Technique",
	"Channeled Energy",
	"Maximum Technique",
	"Domain Expansion",
	"Reverse Cursed Technique",
	"Simple Domain",
	"Channel",
	"Part",
	"Shikigami",
	"Binding Vow",
	"Reveal",
}

// KeywordPrefix reports whether words begins with a themed keyword, returning
// that keyword and how many words it spans. The longest keyword wins, so a
// keyword that is a prefix of another can never mask it.
func KeywordPrefix(words []string) (keyword string, n int, ok bool) {
	for _, kw := range Keywords {
		kwWords := strings.Fields(kw)
		if len(kwWords) <= n || len(words) < len(kwWords) {
			continue
		}
		if slices.EqualFunc(kwWords, words[:len(kwWords)], strings.EqualFold) {
			keyword, n, ok = kw, len(kwWords), true
		}
	}
	return keyword, n, ok
}

// Program is an ordered list of pipeline statements, plus any Shikigami
// (user-defined operation) definitions and `Innate Domain` imports collected
// from the source.
type Program struct {
	Statements []*Statement
	Shikigamis []*ShikigamiDef
	Imports    []*Import
}

// Import is an `Innate Domain: <target>` statement: a Shikigami library to
// load before this program. Like Shikigami definitions, imports are hoisted out
// of the statement list — they are declarations, not pipeline steps, so where
// they appear in the file does not matter.
//
// Target is written without the `.domain` extension, matching how a
// `Cursed Energy:` target is written as a bare path.
type Import struct {
	Target string
	Pos    token.Position
}

// Param is a typed Shikigami parameter, e.g. `k: Int` or
// `p: (Int) -> Bool`.
type Param struct {
	Name string
	Type *TypeExpr
	Pos  token.Position
}

// TypeExpr is a type as *written*: a syntactic tree, deliberately not an
// ir.Type. Lowering it (prims.lowerTypeExpr) is where names are checked and
// keyability is enforced, which keeps the parser free of the type model.
//
// Exactly one shape is set:
//
//	Name != ""     a named type, with Args for generics: Int, List<Int>, Map<K,V>
//	Tuple != nil   a positional tuple: (Int, Int)
//	Fields != nil  a record: {a: Int, b: Text}
//	Lambda != nil  a lambda type: (Int) -> Bool
type TypeExpr struct {
	Name   string
	Args   []*TypeExpr
	Tuple  []*TypeExpr
	Fields []TypeField
	Lambda *LambdaType
	Pos    token.Position
}

// TypeField is one named member of a written record type. The order is the
// order it was written; ir.Type.Equal compares records by field set, so a
// declared {b: Text, a: Int} still matches an inferred {a: Int, b: Text}.
type TypeField struct {
	Name string
	Type *TypeExpr
	Pos  token.Position
}

// LambdaType is the type of a lambda-valued Shikigami parameter.
type LambdaType struct {
	Params []*TypeExpr
	Result *TypeExpr
}

// Signature is a Shikigami's declared pipeline type, `: In -> Out`. It is
// optional: a Shikigami without one is checked at each call site as before,
// which is also how a polymorphic Shikigami stays writable (Domain has no type
// variables, so a declared signature is necessarily monomorphic).
type Signature struct {
	In  *TypeExpr
	Out *TypeExpr
	Pos token.Position
}

// ShikigamiDef is a user-defined, parameterized operation composed from
// primitives:
//
//	Shikigami "Top K Sum" (k: Int)
//	    Domain Expansion: Quicksort, Descending
//	    Maximum Technique: Select Top k, Sum
type ShikigamiDef struct {
	Name   string
	Params []Param
	Sig    *Signature // declared `: In -> Out`; nil when not written
	Binds  []*Binding // `Consider x As/Of …` lines at the top of the body
	Body   []*Statement
	Pos    token.Position
}

// Statement is one node of the themed pipeline layer.
//
//	Keyword: <operation phrase>
//	    Name: <arg value>     # optional indented named arguments
//	    <child statements>    # or an indented sub-pipeline
//
// A statement written without its keyword — a bare operation phrase such as
// `Split Text by "\n"` — parses with Keyword == "". prims.Infer fills the
// keyword in from the phrase before resolution, so every later stage sees a
// fully-keyworded statement and behaves identically either way —
// KeywordInferred is how a tool that wants to speak the source's own style
// (a linter's advice, a formatter) can still tell the two apart.
type Statement struct {
	Keyword         string // e.g. "Cursed Energy", "Domain Expansion"; "" until inferred
	KeywordInferred bool   // the source wrote no keyword; Keyword was recovered from Op

	ChannelName string       // for `Channel "name":` statements; "" otherwise
	PartName    string       // for `Part "1":` statements; "" otherwise
	Op          *Operation   // the inline operation phrase (nil for a pure block opener)
	Args        []*Arg       // named arguments from an indented block (Mode:, Using:)
	Binds       []*Binding   // `Consider x As/Of …` lines from an indented block
	Block       []*Statement // an indented sub-pipeline (mutually exclusive with Args)
	// Foreign is the verbatim body of a foreign-language statement
	// (`Domain Expansion: Python` and its siblings). It excludes Args and
	// Block: the indented region beneath such a statement is source in another
	// language, so there is nothing there for either of them to hold.
	Foreign *ForeignBlock
	Pos     token.Position
}

// Operation is a parsed operation phrase: the text after a keyword's colon.
// "Split Text by \"\\n\\n\"" parses to Words=[Split Text by] Strings=["\n\n"].
// "Select Top 3, Sum"        parses to Words=[Select Top] Ints=[3] Modifiers=[Sum].
type Operation struct {
	Words     []string // identifier words of the primary segment, in order
	Strings   []string // string literal arguments in the primary segment
	Ints      []int64  // integer literal arguments in the primary segment
	OpSyms    []string // comparison operators in the primary segment ("<", ">", "=", ...)
	Modifiers []string // comma-separated trailing segments, each joined to a phrase
	Raw       string   // exact original source text of the phrase
	Pos       token.Position
}

// Arg is a named argument supplied as an indented block line: `Name: value`.
//
// Used is set by the resolver when a primitive actually reads the argument
// (prims.ArgSet records every lookup). Nothing in resolution depends on it —
// it exists so the linter can say "this primitive never read that argument",
// which is the only way to catch a `Size:` on a primitive that takes none.
// Like the keyword filled in by prims.Infer, it is a resolve-time annotation
// on the tree rather than part of the parse.
type Arg struct {
	Name  string
	Value ArgValue
	Used  bool
	Pos   token.Position
}

// Binding is a `Consider NAME As <value>` or `Consider NAME Of <source>` line
// written in a statement's indented block: a local variable the expressions on
// that statement (and on the statements nested beneath it) can use by name.
//
// The two prepositions are the whole distinction, and they exist because a
// 1-parameter lambda already means two different things in Domain depending on
// the slot it is written in — a `Using:` lambda is applied per element, a
// measured argument's lambda is applied once to the current pipeline value. A
// binding has no slot to disambiguate it, so the keyword does:
//
//	Consider accum As 3                # a constant
//	Consider len   As (x) -> length(x) # a function: call it as len(xs)
//	Consider total Of Sum              # the current value, summed
//	Consider total Of (xs) -> sum(xs)  # the same, written as a lambda
//	Consider total Of                  # …or as a whole sub-pipeline
//	    Cursed Technique: Map Each
//	        Using: (r) -> r.n
//	    Maximum Technique: Sum
//
// `As` never sees the pipeline value; `Of` always does. Exactly one of Value,
// Lambda and Body is set:
//
//	Of == false, Value  != nil   As <expression>
//	Of == false, Lambda != nil   As <lambda>   — a function, inlined at call sites
//	Of == true,  Lambda != nil   Of <lambda>   — applied to the current value
//	Of == true,  Body   != nil   Of <phrase>, or Of + an indented sub-pipeline
//
// Used is set by the resolver when something actually reads the binding, for
// the same reason Arg.Used exists: so the linter can say a binding has no
// effect rather than letting it sit there looking load-bearing.
type Binding struct {
	Name   string
	Of     bool
	Value  Expr
	Lambda *Lambda
	Body   []*Statement
	Used   bool
	Pos    token.Position

	// Identity marks `Consider x Of Itself`: the binding is the value
	// entering the scope, unchanged. It is a word rather than a bare
	// `Consider x Of`, because that spelling is how the REPL knows an
	// indented sub-pipeline is still coming — making it complete on its own
	// would cost `Consider mean Of` + a body its continuation prompt.
	Identity bool
}

// ArgValue is the value of a named argument.
type ArgValue interface{ argValue() }

type StringArg struct{ Value string }
type IntArg struct{ Value int64 }
type FloatArg struct{ Value float64 }
type IdentArg struct{ Value string }
type IdentListArg struct{ Values []string } // e.g. From: moves, stacks
type LambdaArg struct{ Lambda *Lambda }

// CaseArg is `Case: <tag> "<template>"` — one alternative of a Match Pattern
// that accepts several line shapes. The tag names which one matched; the
// argument may repeat, and order is priority order.
type CaseArg struct {
	Tag      string
	Template string
}

func (StringArg) argValue()    {}
func (IntArg) argValue()       {}
func (FloatArg) argValue()     {}
func (IdentArg) argValue()     {}
func (IdentListArg) argValue() {}
func (LambdaArg) argValue()    {}
func (CaseArg) argValue()      {}

// Lambda is `(params) -> body`, the only construct that crosses into the
// expression layer from the pipeline layer.
type Lambda struct {
	Params []string
	Body   Expr
	Pos    token.Position
}

// Expr is the plain (non-themed) expression layer.
type Expr interface{ expr() }

type IntLit struct {
	Value int64
	Pos   token.Position
}

// FloatLit is a decimal literal like 3.25. Floats exist in the expression
// layer and named arguments; the themed phrase layer stays integer-only.
type FloatLit struct {
	Value float64
	Pos   token.Position
}

// BoolLit is a boolean literal. The parser never produces one — Domain has no
// true/false keywords — but the optimizer's constant folder does (e.g. folding
// `2 < 3` inside a lambda body), so the downstream layers (typecheck, eval,
// codegen) all accept it.
type BoolLit struct {
	Value bool
	Pos   token.Position
}

type StringLit struct {
	Value string
	Pos   token.Position
}

type Ident struct {
	Name string
	Pos  token.Position
}

type BinaryExpr struct {
	Op    token.Kind
	Left  Expr
	Right Expr
	Pos   token.Position
}

type UnaryExpr struct {
	Op  token.Kind
	X   Expr
	Pos token.Position
}

type FieldAccess struct {
	Target Expr
	Field  string
	Pos    token.Position
}

type CallExpr struct {
	Fn   Expr
	Args []Expr
	Pos  token.Position

	// InPlace marks a collection update (insert, del, put, setat) whose
	// receiver the optimizer proved dead: nothing can read the copied-from
	// value after this call, so both backends may write through it instead of
	// copying it. Every update in Domain is *semantically* functional and this
	// does not change that — see optimizer/linear.go for what has to be true
	// before it is set, and why a Fold clones its accumulator once on entry.
	//
	// It is set by the last pass to run, after the rewrite cascade has reached
	// its fixpoint, because a pass that duplicates or reorders a subexpression
	// would invalidate the proof. The two places the optimizer rebuilds a
	// CallExpr carry the flag across anyway, so that ordering is a policy
	// rather than the only thing keeping this correct.
	InPlace bool

	// Braced marks a call the source wrote as a record literal — `{a: 1}`
	// rather than `record("a", 1)`. The parser desugars the literal to this
	// call, so the two are the same node and every layer below the formatter
	// treats them identically; the flag exists only so `domain fmt` gives back
	// the syntax that was written.
	//
	// Desugaring at the parser rather than adding an ast.Expr node is
	// deliberate. Several expression walkers — prims/locals.go's rewriteExpr,
	// collectIdents and renameIdents — fall through on a node kind they do not
	// know, silently and without a compile error, which for a literal holding
	// sub-expressions would mean skipped substitutions and captured names. A
	// CallExpr is a shape all of them have handled since record() shipped.
	//
	// It is presentation only: nothing compares, hashes or dispatches on it,
	// and a pass that rebuilds the call is free to drop it.
	Braced bool
}

// CondExpr is `if cond then a else b`. Both arms are lazy: only the selected
// arm is evaluated, so guard idioms like
// `if length(xs) = 0 then 0 else first(xs)` are safe.
type CondExpr struct {
	Cond Expr
	Then Expr
	Else Expr
	Pos  token.Position
}

// LetExpr is `consider NAME as VALUE in BODY`: a local binding, so a
// subexpression can be named instead of written twice. NAME is in scope only
// inside BODY, and shadows an outer binding of the same name.
type LetExpr struct {
	Name  string
	Value Expr
	Body  Expr
	Pos   token.Position
}

// AssignExpr is `NAME := VALUE`: an update to a local already in scope, whose
// own value is the value written. Domain has no declaration form — the name
// must already be bound by a `consider … as … in …` (LetExpr) or by a
// pipeline-layer `Consider … As/Of …` binding, and everything else (a lambda
// parameter, a function binding, a Shikigami parameter, a builtin) is refused
// at resolve time.
//
// Which of the two it is decides how long the write lives. A `consider` local
// dies with the expression it was written in, so the mutation cannot escape
// one evaluation of the lambda. A stage binding lives for the whole stage, so
// a write is visible to the *next* element and across the laps of a loop —
// which is what makes it real state, and why the resolver stops folding such a
// binding into a literal and the optimizer stands its rewrites down (see
// prims/locals.go and optimizer/walk.go).
type AssignExpr struct {
	Name  string
	Value Expr
	Pos   token.Position
}

// AlsoExpr is `BODY also C1, C2, …`: the clauses are evaluated after the body,
// in written order, and their values are discarded — the expression's value is
// the body's. Written for the effects of the assignments inside them.
//
// It is deliberately not an argument-position form: the clause list is
// comma-separated, so inside a call's argument list it would be ambiguous with
// the arguments' own commas, and the parser says so rather than guessing.
type AlsoExpr struct {
	Body    Expr
	Clauses []Expr
	Pos     token.Position
}

func (*IntLit) expr()      {}
func (*FloatLit) expr()    {}
func (*BoolLit) expr()     {}
func (*StringLit) expr()   {}
func (*Ident) expr()       {}
func (*BinaryExpr) expr()  {}
func (*UnaryExpr) expr()   {}
func (*FieldAccess) expr() {}
func (*CallExpr) expr()    {}
func (*CondExpr) expr()    {}
func (*LetExpr) expr()     {}
func (*AssignExpr) expr()  {}
func (*AlsoExpr) expr()    {}
