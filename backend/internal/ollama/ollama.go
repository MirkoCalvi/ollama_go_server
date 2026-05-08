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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64  `json:"temperature,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// defaultStop hard-cuts generation when the model starts drifting into
// document-completion mode (markdown headers, simulated dialogues, role labels).
// Belt-and-braces companion to the "Output format" rules in each system prompt.
var defaultStop = []string{
	"\n---",
	"\n\n**",
	"\n# ",
	"\n## ",
	"Simulated Conversation",
	"User:",
	"Assistant:",
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options,omitempty"`
}

type chatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`

	Done bool `json:"done"`
}

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

// Generate sends prompt to Ollama and returns the model's response text.
// The caller's ctx governs the timeout.
func (c *Client) Chat(
	ctx context.Context,
	character *Character,
	history []chatMessage,
	userMessage string,
) (string, error) {

	messages := []chatMessage{
		{
			Role:    "system",
			Content: character.SystemPrompt,
		},
	}

	messages = append(messages, history...)

	messages = append(messages, chatMessage{
		Role:    "user",
		Content: userMessage,
	})

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,

		Options: chatOptions{
			Temperature: character.Parameters.Temperature,
			TopP:        character.Parameters.TopP,
			TopK:        character.Parameters.TopK,
			Stop:        defaultStop,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/chat",
		bytes.NewReader(body),
	)
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf(
			"ollama returned status %d: %s",
			resp.StatusCode,
			string(b),
		)
	}

	var out chatResponse

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return out.Message.Content, nil
}
