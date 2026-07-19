package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildBinary compiles generated Go source into a static binary at outPath by
// writing a throwaway module and shelling out to the Go toolchain.
func BuildBinary(goSrc, outPath string) error {
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
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return err
	}
	gomod := "module domainprog\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}

	cmd := exec.Command(goTool, "build", "-trimpath", "-ldflags", "-s -w", "-o", absOut, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, out)
	}
	return nil
}
