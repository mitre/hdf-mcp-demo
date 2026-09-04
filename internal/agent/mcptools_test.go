package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

// TestToolBox_SubsetAdvertisesOnlyRequested pins the mechanism uqhe.18 needs: an
// arm can advertise an arbitrary subset of the read tools, and the cost of what
// it advertises is measurable. Without the second half, "a smaller surface is
// cheaper" stays an assumption rather than a number.
func TestToolBox_SubsetAdvertisesOnlyRequested(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf to run the toolbox subset test")
		}
		bin = p
	}
	root := t.TempDir()
	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(context.Background(), bin, env)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()

	full, err := NewMCPToolBox(context.Background(), sess, ReadTools)
	if err != nil {
		t.Fatal(err)
	}
	minimal, err := NewMCPToolBox(context.Background(), sess, map[string]bool{"hdf_open": true, "hdf_query": true})
	if err != nil {
		t.Fatal(err)
	}

	if got := len(minimal.Definitions()); got != 2 {
		t.Errorf("minimal toolbox advertises %d tools, want 2", got)
	}
	for _, d := range minimal.Definitions() {
		if d.Name != "hdf_open" && d.Name != "hdf_query" {
			t.Errorf("minimal toolbox advertised an unrequested tool: %s", d.Name)
		}
	}

	fullCost, minCost := full.SchemaTokens(), minimal.SchemaTokens()
	if fullCost <= 0 || minCost <= 0 {
		t.Fatalf("schema cost must be measurable, got full=%d minimal=%d", fullCost, minCost)
	}
	if minCost >= fullCost {
		t.Errorf("a smaller surface must cost less: minimal=%d full=%d", minCost, fullCost)
	}
	t.Logf("advertised schema cost: full(%d tools)=%d tokens, minimal(2 tools)=%d tokens",
		len(full.Definitions()), fullCost, minCost)
}
