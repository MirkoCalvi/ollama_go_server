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
	"time"
)

// defaultNumPredict caps the number of tokens the model may generate per chat.
// Without this, a verbose response on slow (e.g. CPU-only) hardware can
// outlast JOB_TIMEOUT_SECONDS and the worker context cancels mid-generation.
// 512 tokens is generous for a chat reply yet fits comfortably inside the
// default 180s budget even at ~5 tokens/sec.
const defaultNumPredict = 512

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64  `json:"temperature,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ChatStats is Ollama's per-call timing breakdown, useful for diagnosing
// slow responses (cold-start load vs. prompt eval vs. token generation).
type ChatStats struct {
	TotalDuration      time.Duration
	LoadDuration       time.Duration
	PromptEvalCount    int
	PromptEvalDuration time.Duration
	EvalCount          int
	EvalDuration       time.Duration
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

	// Timing fields are reported by Ollama in nanoseconds and decode
	// directly into time.Duration (underlying type int64).
	TotalDuration      time.Duration `json:"total_duration"`
	LoadDuration       time.Duration `json:"load_duration"`
	PromptEvalCount    int           `json:"prompt_eval_count"`
	PromptEvalDuration time.Duration `json:"prompt_eval_duration"`
	EvalCount          int           `json:"eval_count"`
	EvalDuration       time.Duration `json:"eval_duration"`
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

// Chat sends a chat completion request to Ollama and returns the assistant's
// reply along with Ollama's per-call timing breakdown. The caller's ctx
// governs the timeout. ChatStats is zero-valued when the call fails before
// the response is decoded.
func (c *Client) Chat(
	ctx context.Context,
	character *Character,
	history []chatMessage,
	userMessage string,
) (string, ChatStats, error) {

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
			NumPredict:  defaultNumPredict,
			Stop:        defaultStop,
		},
	})
	if err != nil {
		return "", ChatStats{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/chat",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", ChatStats{}, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", ChatStats{}, fmt.Errorf("call ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", ChatStats{}, fmt.Errorf(
			"ollama returned status %d: %s",
			resp.StatusCode,
			string(b),
		)
	}

	var out chatResponse

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", ChatStats{}, fmt.Errorf("decode response: %w", err)
	}

	stats := ChatStats{
		TotalDuration:      out.TotalDuration,
		LoadDuration:       out.LoadDuration,
		PromptEvalCount:    out.PromptEvalCount,
		PromptEvalDuration: out.PromptEvalDuration,
		EvalCount:          out.EvalCount,
		EvalDuration:       out.EvalDuration,
	}

	return out.Message.Content, stats, nil
}
