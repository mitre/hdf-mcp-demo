package aibom

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sampleModel is the metadata Ollama actually reports for a pulled model —
// nothing here is invented.
func sampleModel() Model {
	return Model{
		Name:           "granite4.1:8b",
		Digest:         "sha256:444af1c4b2fe",
		Family:         "granite",
		Architecture:   "granite",
		ParameterSize:  "8.8B",
		ParameterCount: 8791592960,
		Quantization:   "Q4_K_M",
		Format:         "gguf",
		License:        "apache-2.0",
		Capabilities:   []string{"completion", "tools"},
	}
}

// TestAIBOM_FromModel pins the emitted document's shape: a CycloneDX ML-BOM whose
// single machine-learning-model component carries the model's real identity.
func TestAIBOM_FromModel(t *testing.T) {
	b, err := CycloneDX(sampleModel())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("emitted document is not valid JSON: %v", err)
	}
	if doc["bomFormat"] != "CycloneDX" {
		t.Errorf("bomFormat = %v, want CycloneDX", doc["bomFormat"])
	}
	comps, _ := doc["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("want exactly one component, got %d", len(comps))
	}
	c, _ := comps[0].(map[string]any)
	if c["type"] != "machine-learning-model" {
		t.Errorf("component type = %v, want machine-learning-model (this is what makes it an ML-BOM)", c["type"])
	}
	if c["name"] != "granite4.1:8b" {
		t.Errorf("component name = %v, want the model name", c["name"])
	}
	card, _ := c["modelCard"].(map[string]any)
	params, _ := card["modelParameters"].(map[string]any)
	if params["modelArchitecture"] != "granite" {
		t.Errorf("modelArchitecture = %v, want the architecture Ollama reported", params["modelArchitecture"])
	}
}

// TestAIBOM_OmitsWhatOllamaDoesNotReport is the card's central anti-pattern:
// unknown fields are left UNSET, never invented. Ollama reports no training
// data, no task, no learning approach and no performance metrics, so an emitted
// document must not claim any.
func TestAIBOM_OmitsWhatOllamaDoesNotReport(t *testing.T) {
	b, err := CycloneDX(sampleModel())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"datasets", "quantitativeAnalysis", "considerations", "task", "approach"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("emitted document contains %q — Ollama does not report it, so it must be omitted, not fabricated:\n%s", forbidden, b)
		}
	}
}

// TestAIBOM_PartialMetadata covers a model whose optional fields are missing:
// the emitter must degrade to what it has rather than emitting empty strings a
// consumer would read as real values.
func TestAIBOM_PartialMetadata(t *testing.T) {
	b, err := CycloneDX(Model{Name: "tiny:1b"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, forbidden := range []string{`""`, `"licenses"`, `"modelCard"`} {
		if strings.Contains(s, forbidden) {
			t.Errorf("emitted document carries %s for a model with no such metadata:\n%s", forbidden, s)
		}
	}
}

// TestAIBOM_IngestedByHDF is the acceptance test that matters: an emitted
// document must be something the shipped hdf binary recognizes AS an ML-BOM and
// turns into a schema-valid HDF System. Emitting well-formed JSON that HDF
// rejects would satisfy every other test here and be useless.
func TestAIBOM_IngestedByHDF(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the ingestion test")
		}
		bin = p
	}
	dir := t.TempDir()
	bom := filepath.Join(dir, "model.cdx.json")
	out := filepath.Join(dir, "model.system.json")

	b, err := CycloneDX(sampleModel())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bom, b, 0o600); err != nil {
		t.Fatal(err)
	}

	// --from asserts the format: hdf detects the input and REJECTS a mismatch, so
	// this passing proves the document is genuinely detected as an ML-BOM rather
	// than merely parsed as some other BOM shape.
	cmd := exec.Command(bin, "system", "create", bom, "--from", "cyclonedx-mlbom", "-o", out)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hdf system create --from cyclonedx-mlbom rejected the emitted document: %v\n%s", err, combined)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no System document written: %v", err)
	}
	var sys struct {
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(got, &sys); err != nil {
		t.Fatal(err)
	}
	if len(sys.Components) == 0 {
		t.Fatalf("System document has no components:\n%s", got)
	}
}
