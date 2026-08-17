package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

// TestTwoArm_Live runs ONE question through both arms against real model(s), so
// the loop can be seen end to end before it is built out. Gated on an
// OpenAI-compatible endpoint (OPENAI_BASE_URL + OPENAI_MODEL or OPENAI_MODEL_LIST
// [+ OPENAI_API_KEY]) and a built hdf binary (HDF_BIN, or hdf on PATH). One
// sub-test per model. For a multi-model run that includes a slow reasoning model,
// raise the go test timeout, e.g.:
//
//	export OPENAI_BASE_URL=… OPENAI_MODEL_LIST="gemma-4,gpt-oss-120b" OPENAI_API_KEY=…
//	export HDF_BIN=/path/to/hdf
//	go test ./internal/agent/ -run TwoArm_Live -v -timeout 30m
func TestTwoArm_Live(t *testing.T) {
	base, models := os.Getenv("OPENAI_BASE_URL"), instrument.ModelsFromEnv()
	if base == "" || len(models) == 0 {
		t.Skip("set OPENAI_BASE_URL and OPENAI_MODEL or OPENAI_MODEL_LIST (and OPENAI_API_KEY) to run the live two-arm smoke")
	}
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		if p, err := exec.LookPath("hdf"); err == nil {
			bin = p
		} else {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the HDF arm")
		}
	}

	// Shared, model-independent setup: stage the raw fixture, connect one session
	// (kept alive for the whole test), normalize gosec.json -> gosec.hdf.json, and
	// build both tool surfaces.
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "gosec.json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "gosec.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "HDF_MCP_ROOT="+root, "HDF_MCP_ENABLE_WRITES=1")
	sess, err := mcpclient.Connect(context.Background(), bin, env)
	if err != nil {
		t.Fatalf("connect hdf mcp: %v", err)
	}
	defer func() { _ = sess.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 2*time.Minute)
	if _, err := sess.Call(setupCtx, "hdf_convert", map[string]any{
		"source": map[string]any{"path": "gosec.json"}, "from": "gosec", "output": "gosec.hdf.json",
	}); err != nil {
		cancelSetup()
		t.Fatalf("normalize gosec: %v", err)
	}
	rawTB := NewRawFileToolBox(root)
	mcpTB, err := NewMCPToolBox(setupCtx, sess, ReadTools)
	cancelSetup()
	if err != nil {
		t.Fatalf("mcp toolbox: %v", err)
	}

	const system = "You are a security analyst. Use the available tools to get the data you need, then answer the user's question with a specific number."

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			inst := instrument.NewOpenAI(base, model, os.Getenv("OPENAI_API_KEY"))
			inst.MaxTokens = 1024

			// Generous per-model window: a cold reasoning-model load can be minutes.
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()

			armA, errA := Run(ctx, inst, rawTB,
				system, "How many failed findings are in the gosec scan? The scan file is named gosec.json.", 6)
			if errA != nil {
				t.Fatalf("arm A (raw-file): %v", errA)
			}
			armB, errB := Run(ctx, inst, mcpTB,
				system, "How many failed findings are in the gosec results? The HDF document is named gosec.hdf.json.", 6)
			if errB != nil {
				t.Fatalf("arm B (HDF MCP): %v", errB)
			}

			t.Logf("\nmodel: %s", inst.Name())
			t.Logf("%-14s %-10s %8s %8s %8s %6s  %s", "arm", "total tok", "prompt", "compl", "toolcalls", "iters", "answer")
			logArm(t, "raw-file", armA)
			logArm(t, "hdf-mcp", armB)

			if armA.Answer == "" || armB.Answer == "" {
				t.Errorf("both arms should produce an answer (A=%q B=%q)", armA.Answer, armB.Answer)
			}
			if armA.ToolCalls == 0 || armB.ToolCalls == 0 {
				t.Errorf("both arms should call a tool (A=%d B=%d)", armA.ToolCalls, armB.ToolCalls)
			}
		})
	}
}

func logArm(t *testing.T, name string, r Result) {
	t.Helper()
	ans := r.Answer
	if len(ans) > 80 {
		ans = ans[:77] + "…"
	}
	t.Logf("%-14s %-10d %8d %8d %8d %6d  %q (%.1fs)",
		name, r.TotalTokens(), r.PromptTokens, r.CompletionTokens, r.ToolCalls, r.Iterations, ans, r.Elapsed.Seconds())
}
