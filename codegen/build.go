package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildConfig varies how the Go toolchain is invoked.
//
// The defaults are the contract `domain build` publishes and the one
// bench/README.md measures against: -trimpath, stripped, CGO off. They are not
// negotiable for an ordinary build, because a binary built differently is not
// comparable with the numbers this repo publishes.
//
// It exists for `domain expansion: mahoraga`, which is in the business of
// asking whether *this* program wants something else — a profile-guided
// rebuild, a newer instruction set — and can answer only by building it both
// ways and measuring. Anything it turns on is recorded in the recipe, so a
// binary never ends up faster for reasons nobody wrote down.
type BuildConfig struct {
	// Flags are appended to `go build` after the defaults, so a caller can add
	// -pgo or -gcflags without restating them.
	Flags []string

	// Env entries ("GOAMD64=v3") are appended to the environment, after
	// CGO_ENABLED=0, so a caller may also override that if it means to.
	Env []string
}

// BuildBinary compiles generated Go source into a static binary at outPath by
// writing a throwaway module and shelling out to the Go toolchain.
func BuildBinary(goSrc, outPath string) error {
	return BuildBinaryWith(goSrc, outPath, BuildConfig{})
}

// BuildBinaryWith is BuildBinary with extra toolchain flags and environment.
func BuildBinaryWith(goSrc, outPath string, cfg BuildConfig) error {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("the Go toolchain is required to build binaries: %w", err)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "domain-build-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return err
	}
	// What the program imports decides what it is built with. A program that
	// names no module gets the bare go.mod and the flags this backend has
	// always used; one that does gets the repository's own requirements and
	// hashes. See codegen/deps.go.
	gomod, gosum, err := moduleFiles(goSrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	if gosum != "" {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte(gosum), 0o644); err != nil {
			return err
		}
	}

	cmd := exec.Command(goTool, buildArgs(absOut, cfg)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), buildEnv(gosum != "", cfg)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, out)
	}
	return nil
}

// buildArgs and buildEnv are the toolchain invocation, split out so that the
// promise they carry can be tested rather than merely intended: a program that
// names no module is built with the same arguments and the same environment
// this backend has always used. See TestStdlibBuildInputsAreUnchanged.

func buildArgs(absOut string, cfg BuildConfig) []string {
	args := []string{"build", "-trimpath", "-ldflags", "-s -w"}
	args = append(args, cfg.Flags...)
	return append(args, "-o", absOut, ".")
}

func buildEnv(modules bool, cfg BuildConfig) []string {
	env := []string{"CGO_ENABLED=0"}
	if modules {
		// -mod=mod lets the toolchain settle the generated go.mod — mark an
		// indirect requirement direct, drop one the program does not reach.
		// Everything it needs is already in the module cache and in the
		// go.sum written beside it, so nothing is fetched. A stdlib-only
		// build never sets this, which is what keeps its command line the one
		// bench/ measured.
		env = append(env, "GOFLAGS=-mod=mod")
	}
	return append(env, cfg.Env...)
}
