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
	gomod := "module domainprog\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}

	args := []string{"build", "-trimpath", "-ldflags", "-s -w"}
	args = append(args, cfg.Flags...)
	args = append(args, "-o", absOut, ".")

	cmd := exec.Command(goTool, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, cfg.Env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, out)
	}
	return nil
}
