// Package ast defines the parsed tree for Domain. It distinguishes the two
// syntactic layers: the themed pipeline layer (Statement / Operation) and the
// plain expression layer (Expr / Lambda), which appears inside Using: lambdas
// and Binding Vow predicates.
package ast

import "domain/token"

// Program is an ordered list of pipeline statements, plus any Shikigami
// (user-defined operation) definitions collected from the source.
type Program struct {
	Statements []*Statement
	Shikigamis []*ShikigamiDef
}

// Param is a typed Shikigami parameter, e.g. `k: Int`.
type Param struct {
	Name string
	Type string // "Int" or "Text" in v0.2
	Pos  token.Position
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
	Body   []*Statement
	Pos    token.Position
}

// Statement is one node of the themed pipeline layer.
//
//	Keyword: <operation phrase>
//	    Name: <arg value>     # optional indented named arguments
//	    <child statements>    # or an indented sub-pipeline
type Statement struct {
	Keyword     string       // e.g. "Cursed Energy", "Domain Expansion"
	ChannelName string       // for `Channel "name":` statements; "" otherwise
	Op          *Operation   // the inline operation phrase (nil for a pure block opener)
	Args        []*Arg       // named arguments from an indented block (Mode:, Using:)
	Block       []*Statement // an indented sub-pipeline (mutually exclusive with Args)
	Pos         token.Position
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
type Arg struct {
	Name  string
	Value ArgValue
	Pos   token.Position
}

// ArgValue is the value of a named argument.
type ArgValue interface{ argValue() }

type StringArg struct{ Value string }
type IntArg struct{ Value int64 }
type FloatArg struct{ Value float64 }
type IdentArg struct{ Value string }
type IdentListArg struct{ Values []string } // e.g. From: moves, stacks
type LambdaArg struct{ Lambda *Lambda }

func (StringArg) argValue()    {}
func (IntArg) argValue()       {}
func (FloatArg) argValue()     {}
func (IdentArg) argValue()     {}
func (IdentListArg) argValue() {}
func (LambdaArg) argValue()    {}

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
	Op   token.Kind
	X    Expr
	Pos  token.Position
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
