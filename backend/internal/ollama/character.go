// See docs/PEROSNALITY.md
// This LLM is designed to be frigid, cynic and angry by default.
package ollama

// PersonalityParameters carries the per-character sampling knobs forwarded to
// Ollama's chat options. Fields are exported so character literals can live
// in sibling packages (e.g. internal/ollama/personalities).
type PersonalityParameters struct {
	Temperature float64
	TopP        float64
	TopK        int
}

// Character pairs a sampling profile with a system prompt that defines the
// persona. Concrete characters are defined in internal/ollama/personalities.
//
// Description is a short, user-facing blurb (one sentence) shown on the
// frontend homepage. It is the only persona text the public /characters
// endpoint exposes — the SystemPrompt and Parameters stay internal.
type Character struct {
	Name         string
	Description  string
	Parameters   PersonalityParameters
	SystemPrompt string
}
