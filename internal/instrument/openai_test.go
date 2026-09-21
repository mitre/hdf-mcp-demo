package instrument

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// TestOpenAI_RequestTimeoutIsConfigurable pins the knob a slow reasoning model
// needs. A response that outlives the request cap fails as a transport error, so
// the message must NAME the cap: a report reader seeing only "context deadline
// exceeded" cannot tell a model that thought for too long from an endpoint that
// is simply down. The attempt count is asserted too, because the retry loop
// treats a transport failure as retryable — with the hidden five-minute literal
// that meant up to twenty-five minutes of wall clock inside one arm.
func TestOpenAI_RequestTimeoutIsConfigurable(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		time.Sleep(300 * time.Millisecond) // outlives the 50ms cap below
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"too late"}}]}`)
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL+"/v1", "slow-model", "")
	c.SetRequestTimeout(50 * time.Millisecond)
	c.MaxRetries = 1 // one retry: two attempts, and no more

	if got := c.HTTP.Timeout; got != 50*time.Millisecond {
		t.Fatalf("configured request timeout = %s, want 50ms", got)
	}
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "think hard"}}, nil)
	if err == nil {
		t.Fatal("want an error when the response outlives the request timeout")
	}
	if !strings.Contains(err.Error(), "50ms") {
		t.Errorf("error %q does not name the configured request timeout", err)
	}
	if !strings.Contains(err.Error(), "request timeout") {
		t.Errorf("error %q does not say the request timeout is what elapsed", err)
	}
	if n := atomic.LoadInt32(&attempts); n != 2 {
		t.Errorf("endpoint saw %d attempts, want 2 (the initial one plus MaxRetries=1)", n)
	}
}

// TestRequestTimeoutDefault pins the default at five minutes for BOTH instruments
// — exposing the knob must not change what an existing run does — and pins that a
// non-positive value falls back to it rather than meaning "no cap". The cap exists
// so a hung connection fails; it is made visible here, not removed.
func TestRequestTimeoutDefault(t *testing.T) {
	if DefaultRequestTimeout != 5*time.Minute {
		t.Errorf("DefaultRequestTimeout = %s, want 5m0s", DefaultRequestTimeout)
	}
	oa, ol := NewOpenAI("http://x/v1", "m", "k"), NewOllama("", "m")
	if oa.HTTP.Timeout != DefaultRequestTimeout {
		t.Errorf("NewOpenAI request timeout = %s, want %s", oa.HTTP.Timeout, DefaultRequestTimeout)
	}
	if ol.HTTP.Timeout != DefaultRequestTimeout {
		t.Errorf("NewOllama request timeout = %s, want %s", ol.HTTP.Timeout, DefaultRequestTimeout)
	}
	for _, d := range []time.Duration{0, -time.Second} {
		oa.SetRequestTimeout(d)
		ol.SetRequestTimeout(d)
		if oa.HTTP.Timeout != DefaultRequestTimeout || ol.HTTP.Timeout != DefaultRequestTimeout {
			t.Errorf("SetRequestTimeout(%s) gave openai %s / ollama %s, want the %s default",
				d, oa.HTTP.Timeout, ol.HTTP.Timeout, DefaultRequestTimeout)
		}
	}
}

// TestOpenAI_ImplementsInstrument is a compile-time-ish guard that both adapters
// satisfy the interface the study depends on.
func TestOpenAI_ImplementsInstrument(t *testing.T) {
	var _ Instrument = NewOpenAI("http://x/v1", "m", "k")
	var _ Instrument = NewOllama("", "m")
}
