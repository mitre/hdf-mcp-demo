package mcpclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hdfBinary locates a built hdf: $HDF_BIN, else "hdf" on PATH. It skips the test
// if none is runnable — the demo needs the shipped binary, but a units-only CI
// without it should skip, not hard-fail.
func hdfBinary(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("HDF_BIN"); b != "" {
		return b
	}
	if _, err := exec.LookPath("hdf"); err == nil {
		return "hdf"
	}
	t.Skip("no hdf binary found — set HDF_BIN=/path/to/hdf or put hdf on PATH")
	return ""
}

// TestMCPClient_RoundTrip is the card's first failing test: spawn the real
// `hdf mcp`, and round-trip a tool call over stdio, asserting a structured
// response. It converts a vendored real fixture (exercising client → server →
// tool → back) and checks the summary docType.
func TestMCPClient_RoundTrip(t *testing.T) {
	bin := hdfBinary(t)
	root := t.TempDir()
	src, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "gosec.json"))
	if err != nil {
		t.Fatalf("vendored fixture unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "gosec.json"), src, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := Connect(ctx, bin, env)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	payload, err := sess.Call(ctx, "hdf_convert", map[string]any{
		"source": map[string]any{"path": "gosec.json"}, "from": "gosec",
	})
	if err != nil {
		t.Fatalf("hdf_convert round-trip: %v", err)
	}
	if !strings.Contains(payload, `"docType":"results"`) {
		t.Fatalf("expected a results summary from the round-trip, got: %s", payload)
	}
}
