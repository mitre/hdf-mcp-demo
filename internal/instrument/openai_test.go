package instrument

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOpenAI_RequestAndUsage verifies the OpenAI-compatible adapter fully offline
// with an httptest stub — no real model, no token, no network. It asserts the
// request is well-formed (model, messages, tools, Bearer auth) and that the
// provider's usage counts and tool_calls are parsed back into neutral types.
func TestOpenAI_RequestAndUsage(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"role":"assistant","content":"",
			  "tool_calls":[{"id":"call_1","type":"function",
			    "function":{"name":"hdf_query","arguments":"{\"source\":{\"path\":\"x.json\"}}"}}]}}],
			"usage":{"prompt_tokens":42,"completion_tokens":7,"total_tokens":49}
		}`)
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL+"/v1", "some-model", "secret-token")
	c.HTTP = &http.Client{Timeout: 5 * time.Second}

	ctx := context.Background()
	res, err := c.Chat(ctx, []Message{{Role: "user", Content: "how many failed?"}}, []Tool{
		{Name: "hdf_query", Description: "filter requirements", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Request shaping.
	if gotAuth != "Bearer secret-token" {
		t.Errorf("auth header = %q, want Bearer secret-token", gotAuth)
	}
	if gotBody["model"] != "some-model" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if _, ok := gotBody["tools"].([]any); !ok {
		t.Errorf("tools not sent: %v", gotBody["tools"])
	}

	// Response parsing: usage + tool_calls into neutral types.
	if res.PromptTokens != 42 || res.CompletionTokens != 7 {
		t.Errorf("usage = %d/%d, want 42/7", res.PromptTokens, res.CompletionTokens)
	}
	if len(res.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.Message.ToolCalls))
	}
	tc := res.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "hdf_query" {
		t.Errorf("tool call = %+v", tc)
	}
	if !strings.Contains(string(tc.Arguments), `"path":"x.json"`) {
		t.Errorf("arguments not parsed as raw JSON: %s", tc.Arguments)
	}
}

// TestOpenAI_ImplementsInstrument is a compile-time-ish guard that both adapters
// satisfy the interface the study depends on.
func TestOpenAI_ImplementsInstrument(t *testing.T) {
	var _ Instrument = NewOpenAI("http://x/v1", "m", "k")
	var _ Instrument = NewOllama("", "m")
}
