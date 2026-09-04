package instrument

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// reachableOllama returns an Ollama instrument if a local server is up with at
// least one model, else skips. Model: $OLLAMA_MODEL, else the first pulled.
func reachableOllama(t *testing.T) *Ollama {
	t.Helper()
	o := NewOllama(os.Getenv("OLLAMA_HOST"), "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := o.ListModels(ctx)
	if err != nil {
		t.Skipf("no local Ollama reachable (start `ollama serve`): %v", err)
	}
	if len(models) == 0 {
		t.Skip("Ollama is up but no model is pulled — run e.g. `ollama pull qwen2.5:7b`")
	}
	o.Model = models[0]
	if m := os.Getenv("OLLAMA_MODEL"); m != "" {
		o.Model = m
	}
	return o
}

// TestOllama_ReturnsTokenCounts exercises a real local model when one is
// available: a chat turn returns non-empty content and real token counts.
func TestOllama_ReturnsTokenCounts(t *testing.T) {
	o := reachableOllama(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := o.Chat(ctx, []Message{{Role: "user", Content: "Reply with exactly the word: pong"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if strings.TrimSpace(res.Message.Content) == "" {
		t.Error("expected non-empty assistant content")
	}
	if res.PromptTokens <= 0 || res.CompletionTokens <= 0 {
		t.Errorf("expected positive token counts, got %d/%d", res.PromptTokens, res.CompletionTokens)
	}
	t.Logf("model=%s reply=%q prompt=%d completion=%d", o.Model, strings.TrimSpace(res.Message.Content), res.PromptTokens, res.CompletionTokens)
}

// TestOllama_OptionsSent verifies the num_predict/temperature knobs are placed in
// the request's options object (offline; a stub server captures the body).
func TestOllama_OptionsSent(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"prompt_eval_count":1,"eval_count":1}`))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "test-model")
	o.NumPredict = 256
	temp := 0.0
	o.Temperature = &temp
	if _, err := o.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	opts, ok := got["options"].(map[string]any)
	if !ok {
		t.Fatalf("no options object in request body: %v", got)
	}
	if opts["num_predict"] != float64(256) {
		t.Errorf("num_predict = %v, want 256", opts["num_predict"])
	}
	if opts["temperature"] != float64(0) {
		t.Errorf("temperature = %v, want 0", opts["temperature"])
	}
}

// TestOllama_NumCtxSent verifies num_ctx reaches the request when set, and is
// absent when left at 0 — the difference between a run the raw-file arm can
// actually fit in and one Ollama silently truncates to its 4096 default.
func TestOllama_NumCtxSent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		numCtx int
		want   any // nil = key must be absent
	}{
		{"set", 32768, float64(32768)},
		{"unset defers to server", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
				_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"prompt_eval_count":1,"eval_count":1}`))
			}))
			defer srv.Close()

			o := NewOllama(srv.URL, "test-model")
			o.NumCtx = tc.numCtx
			if _, err := o.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
				t.Fatal(err)
			}
			opts, _ := got["options"].(map[string]any)
			if tc.want == nil {
				if _, present := opts["num_ctx"]; present {
					t.Errorf("num_ctx = %v, want absent", opts["num_ctx"])
				}
				return
			}
			if opts["num_ctx"] != tc.want {
				t.Errorf("num_ctx = %v, want %v", opts["num_ctx"], tc.want)
			}
		})
	}
}

// TestOllama_ShowModel checks the metadata read used for run provenance: real
// reported values are surfaced, and fields the server does not report stay zero
// rather than becoming empty strings a consumer would record as fact.
func TestOllama_ShowModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			_, _ = w.Write([]byte(`{
			  "details": {"family":"granite","parameter_size":"8.8B","quantization_level":"Q4_K_M","format":"gguf"},
			  "model_info": {"general.architecture":"granite","general.license":"apache-2.0","general.parameter_count":8791592960},
			  "capabilities": ["completion","tools"]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"granite4.1:8b","digest":"abc123","capabilities":["tools"]}]}`))
	}))
	defer srv.Close()

	got, err := NewOllama(srv.URL, "granite4.1:8b").ShowModel(context.Background(), "granite4.1:8b")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ field, got, want string }{
		{"Name", got.Name, "granite4.1:8b"},
		{"Family", got.Family, "granite"},
		{"Architecture", got.Architecture, "granite"},
		{"ParameterSize", got.ParameterSize, "8.8B"},
		{"Quantization", got.Quantization, "Q4_K_M"},
		{"License", got.License, "apache-2.0"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}
	if got.ParameterCount != 8791592960 {
		t.Errorf("ParameterCount = %d, want 8791592960", got.ParameterCount)
	}
	if len(got.Capabilities) != 2 {
		t.Errorf("Capabilities = %v, want two", got.Capabilities)
	}
}
