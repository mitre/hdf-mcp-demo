package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
)

// RawFileToolBox is the raw-file arm's tool surface: a single read_file tool that
// returns a scan file's full contents, confined to baseDir (only files directly
// in baseDir; no path traversal). This is where an optional nono-wrap hardening
// layer would slot in — the demo keeps it simple by default.
type RawFileToolBox struct {
	baseDir string
}

// NewRawFileToolBox confines reads to baseDir.
func NewRawFileToolBox(baseDir string) *RawFileToolBox { return &RawFileToolBox{baseDir: baseDir} }

// Definitions advertises the read_file tool.
func (b *RawFileToolBox) Definitions() []instrument.Tool {
	return []instrument.Tool{{
		Name:        "read_file",
		Description: "Read a scan file by name and return its full contents.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "the file name, e.g. grype.json"},
			},
			"required": []string{"name"},
		},
	}}
}

// Execute runs a read_file call. The name is reduced to its base, so the model
// can only read files directly under baseDir.
func (b *RawFileToolBox) Execute(_ context.Context, name string, args json.RawMessage) (string, error) {
	if name != "read_file" {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if a.Name == "" {
		return "", fmt.Errorf("missing required argument: name")
	}
	p := filepath.Join(b.baseDir, filepath.Base(a.Name)) // base only — no traversal outside baseDir
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", filepath.Base(a.Name), err)
	}
	return string(data), nil
}
