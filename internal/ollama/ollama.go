// Package ollama is a thin client for the local Ollama HTTP API.
//
// It deliberately exposes only what this server needs (single-shot text
// generation with stream=false). Streaming and other endpoints can be added
// when there is a concrete need.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client wraps an *http.Client with Ollama-specific defaults.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient returns a Client targeted at baseURL (e.g. "http://localhost:11434")
// using the given model name.
//
// No per-request timeout is set on the underlying http.Client because callers
// pass a context.Context to Generate; that context is the single source of
// truth for cancellation and timeouts.
func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{},
	}
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse mirrors the relevant subset of the Ollama response.
// The full response carries timing fields we don't need.
type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Generate sends prompt to Ollama and returns the model's response text.
// The caller's ctx governs the timeout.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read a bounded chunk of the error body so a misbehaving server
		// can't make us allocate unboundedly.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(b))
	}

	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return out.Response, nil
}
