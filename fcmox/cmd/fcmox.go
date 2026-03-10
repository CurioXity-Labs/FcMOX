package main

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed firecracker
var firecrackerBin []byte

// PrepareBinary writes the embedded firecracker binary to a temp file,
// marks it executable, and returns its path.
// The caller is responsible for removing the file when done (e.g. defer os.Remove(path)).
func PrepareBinary() (string, error) {
	tmp, err := os.CreateTemp("", "firecracker-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	defer tmp.Close()

	if _, err := tmp.Write(firecrackerBin); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write firecracker binary: %w", err)
	}

	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("chmod firecracker binary: %w", err)
	}

	return tmp.Name(), nil
}
