// Package aibom emits a CycloneDX ML-BOM describing the model a benchmark run
// used, so a study is self-describing about what produced it.
//
// The governing rule is that nothing is invented. Ollama reports a model's
// identity (name, digest, architecture, parameter count, quantization, license)
// and nothing about its training data, task, or measured performance. A model
// card that asserted those would be a fabricated provenance record, which is
// worse than an absent one — so unreported fields are omitted entirely rather
// than emitted empty, per HDF's partial-fidelity rule.
package aibom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Model is the subset of Ollama's /api/show and /api/tags output that describes
// a model's identity. Every field is optional: a field left blank was not
// reported, and is omitted from the emitted document.
type Model struct {
	Name           string
	Digest         string
	Family         string
	Architecture   string
	ParameterSize  string // human label, e.g. "8.8B"
	ParameterCount int64  // exact count when reported
	Quantization   string
	Format         string // e.g. "gguf"
	License        string
	Capabilities   []string
}

// CycloneDX emits a CycloneDX 1.6 ML-BOM: one machine-learning-model component
// carrying the model's identity. HDF detects the ML-BOM shape from that
// component type, so it is what makes the document ingestible by
// `hdf system create --from cyclonedx-mlbom`.
func CycloneDX(m Model) ([]byte, error) {
	if m.Name == "" {
		return nil, fmt.Errorf("model name is required to emit an AI-BOM")
	}

	component := map[string]any{
		"type":    "machine-learning-model",
		"name":    m.Name,
		"bom-ref": m.Name,
	}
	if m.Digest != "" {
		component["version"] = m.Digest
	}
	if m.License != "" {
		component["licenses"] = []any{map[string]any{"license": map[string]any{"id": m.License}}}
	}

	// The model card carries only what Ollama actually reports. modelParameters
	// is emitted at all only when there is a real architecture to put in it.
	if arch := m.Architecture; arch != "" {
		component["modelCard"] = map[string]any{
			"modelParameters": map[string]any{"modelArchitecture": arch},
		}
	}

	// Everything else Ollama reports goes in properties, which is CycloneDX's
	// mechanism for facts that have no first-class field — rather than forcing
	// them into model-card slots that mean something else.
	var props []any
	addProp := func(k, v string) {
		if v != "" {
			props = append(props, map[string]any{"name": k, "value": v})
		}
	}
	addProp("ollama:family", m.Family)
	addProp("ollama:parameter_size", m.ParameterSize)
	addProp("ollama:quantization_level", m.Quantization)
	addProp("ollama:format", m.Format)
	if m.ParameterCount > 0 {
		addProp("ollama:parameter_count", fmt.Sprintf("%d", m.ParameterCount))
	}
	for _, c := range m.Capabilities {
		addProp("ollama:capability", c)
	}
	if len(props) > 0 {
		component["properties"] = props
	}

	doc := map[string]any{
		"bomFormat":   "CycloneDX",
		"specVersion": "1.6",
		"version":     1,
		"components":  []any{component},
	}
	// A serial number derived from the model digest keeps the document stable
	// across runs of the same model — an invented UUID would change every run
	// and make two identical studies look like different provenance.
	if m.Digest != "" {
		doc["serialNumber"] = "urn:uuid:" + digestUUID(m.Digest)
	}
	return json.MarshalIndent(doc, "", "  ")
}

// digestUUID renders a model digest as a UUID-shaped string. CycloneDX wants a
// urn:uuid serial number; deriving it from the digest keeps it deterministic, so
// re-running the same model produces the same provenance identity instead of a
// fresh random one that would imply a different artifact.
func digestUUID(digest string) string {
	sum := sha256.Sum256([]byte(digest))
	h := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
