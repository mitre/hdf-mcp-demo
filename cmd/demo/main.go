// Command demo runs the HDF-MCP-vs-raw-file token comparison against the shipped
// `hdf mcp` binary and prints the results table. It is model-free and offline.
//
// Usage:
//
//	HDF_BIN=/path/to/hdf go run ./cmd/demo            # or put hdf on PATH
//	go run ./cmd/demo -fixtures ./fixtures
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/demo"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

func main() {
	fixturesDir := flag.String("fixtures", "fixtures", "directory holding the raw source fixtures")
	flag.Parse()

	if err := run(*fixturesDir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(fixturesDir string) error {
	bin, err := locateHDF()
	if err != nil {
		return err
	}
	bank, err := demo.LoadBank()
	if err != nil {
		return err
	}

	root, err := os.MkdirTemp("", "hdf-mcp-demo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(ctx, bin, env)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	rows, err := demo.Run(ctx, sess, bin, fixturesDir, root, bank)
	if err != nil {
		return err
	}

	fmt.Printf("hdf binary: %s\n\n", bin)
	fmt.Print(demo.RenderTable(rows))
	sa := demo.SmallestAdvantage(rows)
	fmt.Printf("\nsmallest HDF advantage: %s (%.1fx)\n", sa.Category, sa.Ratio)
	return nil
}

// locateHDF finds a built hdf binary via $HDF_BIN, then PATH.
func locateHDF() (string, error) {
	if b := os.Getenv("HDF_BIN"); b != "" {
		return b, nil
	}
	if p, err := exec.LookPath("hdf"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no hdf binary found — set HDF_BIN=/path/to/hdf or put hdf on PATH " +
		"(build: cd ../hdf-libs/hdf-cli && go build -o hdf ./cmd/hdf)")
}
