package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// tagsServer stubs Ollama's /api/tags with the given payload.
func tagsServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const twoModels = `{"models":[
  {"name":"gpt-oss:20b","capabilities":["completion","tools"],"details":{"parameter_size":"20B"}},
  {"name":"embed-only:1b","capabilities":["completion"],"details":{"parameter_size":"1B"}}
]}`

// TestCheckOllamaModels covers the preflight cases that otherwise only surface
// mid-run: a model that is not pulled, and one that cannot call tools at all.
func TestCheckOllamaModels(t *testing.T) {
	srv := tagsServer(t, twoModels)

	for _, tc := range []struct {
		name    string
		models  []string
		wantErr string // substring; empty = expect success
	}{
		{"pulled and tool-capable", []string{"gpt-oss:20b"}, ""},
		{"implicit latest tag accepted", []string{"gpt-oss"}, ""},
		{"not pulled", []string{"llama3.2:3b"}, "ollama pull llama3.2:3b"},
		{"no tool support", []string{"embed-only:1b"}, "does not support tool calling"},
		{"reports every bad model at once", []string{"nope:7b", "embed-only:1b"}, "nope:7b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkOllamaModels(context.Background(), srv.URL, tc.models)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
			// A rejection must point at something usable, not just say no.
			if !strings.Contains(err.Error(), "gpt-oss:20b") {
				t.Errorf("error should list the tool-capable models available: %v", err)
			}
		})
	}
}

// TestCheckOllamaModels_ServerDown ensures an unreachable server is named as the
// likely cause rather than surfacing as a bare connection error.
func TestCheckOllamaModels_ServerDown(t *testing.T) {
	srv := tagsServer(t, twoModels)
	url := srv.URL
	srv.Close()

	err := checkOllamaModels(context.Background(), url, []string{"gpt-oss:20b"})
	if err == nil {
		t.Fatal("expected an error from a closed server")
	}
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Errorf("error should suggest checking `ollama serve`: %v", err)
	}
}

// TestDefaultTimeout pins the auto-scaling budget: local runs get a far larger
// per-model allowance than a batching gateway, and both scale with model count.
func TestDefaultTimeout(t *testing.T) {
	if got, want := defaultTimeout("ollama", 4), 4*time.Hour; got != want {
		t.Errorf("ollama/4 = %v, want %v", got, want)
	}
	if got, want := defaultTimeout("openai", 4), time.Hour; got != want {
		t.Errorf("openai/4 = %v, want %v", got, want)
	}
	if defaultTimeout("ollama", 1) <= defaultTimeout("openai", 1) {
		t.Error("local inference should get a larger budget than a gateway")
	}
	if got, want := defaultTimeout("ollama", 0), time.Hour; got != want {
		t.Errorf("a zero model count must still yield a usable budget: got %v, want %v", got, want)
	}
}

// TestDefaultConcurrency pins the local-vs-gateway split: local inference pays a
// per-request KV-cache cost that a batching gateway does not.
func TestDefaultConcurrency(t *testing.T) {
	if got := defaultConcurrency("ollama"); got != 2 {
		t.Errorf("ollama concurrency = %d, want 2", got)
	}
	if defaultConcurrency("openai") <= defaultConcurrency("ollama") {
		t.Error("a batching gateway should allow more in flight than local inference")
	}
}

// TestDefaultPerModel pins stall isolation: locally, one wedged model must not be
// able to consume the whole run's budget. A gateway keeps the older shared-budget
// behaviour, so its runs are not silently truncated by a new cap.
func TestDefaultPerModel(t *testing.T) {
	if got, want := defaultPerModel("ollama"), time.Hour; got != want {
		t.Errorf("ollama per-model = %v, want %v", got, want)
	}
	if got := defaultPerModel("openai"); got != 0 {
		t.Errorf("openai per-model = %v, want 0 (share the overall timeout)", got)
	}
	// The auto overall budget must cover every model's own allowance, or the
	// last model would be cut off before its per-model timeout could apply.
	models := 4
	if defaultTimeout("ollama", models) < time.Duration(models)*defaultPerModel("ollama") {
		t.Error("overall auto timeout is smaller than the sum of per-model allowances")
	}
}
