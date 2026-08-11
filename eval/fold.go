package eval

import (
	"domain/ast"
	"domain/ir"
)

// Constant folding (prims.foldLiteral) evaluates a closed expression while the
// program is still being lowered, so a binding written as a constant can be
// substituted as one. That is the same evaluator running in a very different
// place: in an editor, on every keystroke, over whatever the buffer currently
// says — and the answer is thrown away unless it is a scalar, because only
// scalars have a literal form.
//
// So a fold runs on a budget. `Consider grid As fill(1099511627776, 0)` is a
// legal thing to write and a perfectly ordinary thing to run; what it must not
// do is make the language server reserve sixteen terabytes while someone is
// still typing the line. Over the budget an expression simply does not fold: it
// stays in the program and is computed once when its scope opens, at run time,
// where the limits are the machine's rather than the editor's.
//
// The number is the size of collection or text a fold may build. It is far
// above any constant worth substituting and far below anything an editor would
// notice building.
const maxFoldBuild = 1 << 16

// folding is set for the duration of a fold. eval's state is already
// per-process and single-threaded this way (the binding stack, the tracer, the
// update mode), and the front end runs one program at a time.
var folding bool

// EvalConst evaluates e for constant folding: the ordinary evaluator, under the
// fold budget. An expression that exceeds it returns an error, which is the
// signal to leave the expression alone rather than fold it.
func EvalConst(e ast.Expr) (ir.Value, error) {
	folding = true
	defer func() { folding = false }()
	return EvalExpr(e, nil)
}

// buildLimit is the largest collection or text a single builtin will construct:
// the fold budget while folding, and the physical limit otherwise.
func buildLimit() int64 {
	if folding {
		return maxFoldBuild
	}
	return maxBuildable
}
