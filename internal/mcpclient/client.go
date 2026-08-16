// Package mcpclient is a thin external MCP client over the shipped `hdf mcp`
// binary. It spawns the binary and speaks JSON-RPC over stdio via the public MCP
// SDK — the same path any real consumer uses. The demo depends on nothing
// private to hdf-libs; that constraint (a separate module cannot import
// hdf-cli/internal/*) and the honest "this is how you consume it" demonstration
// are the same thing.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Session is a connected MCP client over a spawned `hdf mcp` process.
type Session struct {
	cs *sdkmcp.ClientSession
}

// Connect spawns `binary mcp` and connects an MCP client over its stdio. env
// entries are "KEY=VALUE" (e.g. "HDF_MCP_ROOT=/path"); pass nil to inherit the
// parent environment. The caller must Close the returned session.
func Connect(ctx context.Context, binary string, env []string) (*Session, error) {
	cmd := exec.Command(binary, "mcp")
	if env != nil {
		cmd.Env = env
	}
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "hdf-mcp-demo", Version: "0.1.0"}, nil)
	cs, err := c.Connect(ctx, &sdkmcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %q mcp over stdio: %w", binary, err)
	}
	return &Session{cs: cs}, nil
}

// Call invokes a tool and returns its structured content as compact JSON — the
// bytes an agent's context actually receives, and what the demo tokenizes. It
// returns an error on an isError tool result.
func (s *Session) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := s.cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", fmt.Errorf("tool %q returned an error: %s", name, firstText(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", fmt.Errorf("marshal %q structured content: %w", name, err)
	}
	return string(b), nil
}

// Close shuts down the session and terminates the spawned process.
func (s *Session) Close() error { return s.cs.Close() }

func firstText(res *sdkmcp.CallToolResult) string {
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
