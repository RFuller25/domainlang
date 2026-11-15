package codegen

import (
	"fmt"

	"domain/ir"
)

// Code generation reports failure by panicking with a failure, and the exported
// entry points (emit) recover it and hand the wrapped error back. It is the
// only thing this package panics with on purpose: a failure is terminal — it
// ends in a message for the user — so threading an error return through every
// emit function would only be bookkeeping. Nothing else is ever recovered: a
// panic of any other type is a bug and keeps its original stack.
//
// A few call sites do not give up on failure (a speculative attempt that falls
// back to another strategy). They use try0..try3, which recover failures only.
type failure struct{ err error }

// fail aborts code generation with err.
func fail(err error) failure { return failure{err} }

// failf aborts code generation with a formatted error.
func failf(format string, args ...any) failure { return failure{fmt.Errorf(format, args...)} }

// unsupported aborts code generation because the backend cannot lower n.
func unsupported(n *ir.Node, format string, args ...any) failure {
	return failure{&UnsupportedError{Prim: n.Prim, Pos: n.Pos, Msg: fmt.Sprintf(format, args...)}}
}

// check aborts code generation if err is non-nil.
func check(err error) {
	if err != nil {
		panic(failure{err})
	}
}

// must returns its value, or aborts code generation if err is non-nil. The
// must family adapts the error-returning functions of other packages.
func must[A any](a A, err error) A {
	if err != nil {
		panic(failure{err})
	}
	return a
}

func must2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(failure{err})
	}
	return a, b
}

func must3[A, B, C any](a A, b B, c C, err error) (A, B, C) {
	if err != nil {
		panic(failure{err})
	}
	return a, b, c
}

// catch runs f and returns the failure it raised, if any. Any other panic
// propagates untouched.
func catch(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			fl, ok := r.(failure)
			if !ok {
				panic(r)
			}
			err = fl.err
		}
	}()
	f()
	return nil
}

func try0(f func()) error { return catch(f) }

func try1[A any](f func() A) (a A, err error) {
	err = catch(func() { a = f() })
	return
}

func try2[A, B any](f func() (A, B)) (a A, b B, err error) {
	err = catch(func() { a, b = f() })
	return
}

func try3[A, B, C any](f func() (A, B, C)) (a A, b B, c C, err error) {
	err = catch(func() { a, b, c = f() })
	return
}

// asUnsupported0..3 run f and, if it fails, re-raise the failure as the
// backend being unable to lower n, with prefix in front of the original
// message: the lambda and type errors underneath read as a reason.
func asUnsupported0(n *ir.Node, prefix string, f func()) {
	if err := try0(f); err != nil {
		panic(unsupported(n, "%s%v", prefix, err))
	}
}

func asUnsupported1[A any](n *ir.Node, prefix string, f func() A) A {
	a, err := try1(f)
	if err != nil {
		panic(unsupported(n, "%s%v", prefix, err))
	}
	return a
}

func asUnsupported2[A, B any](n *ir.Node, prefix string, f func() (A, B)) (A, B) {
	a, b, err := try2(f)
	if err != nil {
		panic(unsupported(n, "%s%v", prefix, err))
	}
	return a, b
}

func asUnsupported3[A, B, C any](n *ir.Node, prefix string, f func() (A, B, C)) (A, B, C) {
	a, b, c, err := try3(f)
	if err != nil {
		panic(unsupported(n, "%s%v", prefix, err))
	}
	return a, b, c
}
