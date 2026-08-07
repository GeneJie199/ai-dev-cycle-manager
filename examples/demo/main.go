// Demo is a thin wrapper around `devcycle demo` for `go run ./examples/demo`.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot locate module root")
		os.Exit(1)
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	args := append([]string{"run", "./cmd/devcycle", "demo"}, os.Args[1:]...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
