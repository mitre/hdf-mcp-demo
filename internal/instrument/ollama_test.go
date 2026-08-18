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
