// Package tok provides offline O200k token counting — the same encoding the HDF
// MCP server budgets its responses against — so the demo's token numbers line up
// with the server's own accounting. tiktoken-go embeds the encoding, so counting
// requires no network.
package tok

import (
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

var (
	once    sync.Once
	codec   tokenizer.Codec
	initErr error
)

func get() (tokenizer.Codec, error) {
	once.Do(func() { codec, initErr = tokenizer.Get(tokenizer.O200kBase) })
	return codec, initErr
}

// Count returns the number of O200k tokens in s.
func Count(s string) (int, error) {
	c, err := get()
	if err != nil {
		return 0, err
	}
	ids, _, err := c.Encode(s)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
