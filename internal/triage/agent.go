package triage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/genai"
)

const (
	// modelName is the Gemini model used for image triage.
	modelName = "gemini-2.5-flash"

	// triageTimeout is the maximum time allowed for a single Gemini API call.
	triageTimeout = 10 * time.Second

	// systemInstruction is the prompt that steers the model toward structured
	// civic-hazard classification output.
	systemInstruction = `You are an infrastructure hazard classifier for a civic reporting platform.
Analyze the provided image and location, then respond ONLY with valid JSON
matching this schema (no markdown, no explanation):
{
  "category":    "<one of: Pothole | Water Clogging | Drain Overflow | Electrical Hazard | Other>",
  "title":       "<concise title, max 100 chars>",
  "description": "<detailed description, max 500 chars>"
}`
)

// ErrTimeout is returned when the Gemini API call exceeds triageTimeout.
// The HTTP handler should map this to HTTP 504.
var ErrTimeout = errors.New("triage: gemini API request timed out")

// ErrAPIFailure is returned when the Gemini API responds with a non-OK status
// or returns an unrecoverable error.
// The HTTP handler should map this to HTTP 422.
var ErrAPIFailure = errors.New("triage: gemini API request failed")

// TriageResult holds the structured classification returned by the AI triage
// pipeline.  It mirrors the JSON schema the model is prompted to produce.
type TriageResult struct {
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Agent wraps an initialised Gemini client and exposes a single Triage method
// that classifies an infrastructure image.
type Agent struct {
	client *genai.Client
}

// NewAgent creates a new Agent by initialising the Gemini client with the
// supplied API key.  It must be called during application startup (Req 11.3).
// If initialisation fails the error is returned so the caller can refuse to
// serve traffic (Req 11.5).
func NewAgent(ctx context.Context, apiKey string) (*Agent, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("triage: failed to initialise Gemini client: %w", err)
	}
	return &Agent{client: client}, nil
}

// Triage sends a multimodal prompt (image URL + GPS coordinates) to Gemini
// 2.5 Flash and returns the raw text of the model response.
//
// The caller (task 6.2 / parser.go) is responsible for JSON-parsing the raw
// string into a *TriageResult.
//
// Errors:
//   - ErrTimeout  – context deadline exceeded after 10 s  → HTTP 504
//   - ErrAPIFailure – any other Gemini API error           → HTTP 422
func (a *Agent) Triage(ctx context.Context, imageURL string, lat, lng float64) (string, error) {
	// Apply a 10-second deadline on top of whatever context the caller passed.
	timedCtx, cancel := context.WithTimeout(ctx, triageTimeout)
	defer cancel()

	// Build the user turn: image part + location text + task instruction.
	userParts := []*genai.Part{
		// Pass the Cloud Storage image URL as a FileData URI part.
		// Gemini 2.5 Flash can fetch images directly from Cloud Storage URLs.
		genai.NewPartFromURI(imageURL, "image/jpeg"),
		genai.NewPartFromText(
			fmt.Sprintf(
				"Location: latitude=%.6f, longitude=%.6f\nClassify the infrastructure hazard shown in this image.",
				lat, lng,
			),
		),
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(userParts, genai.RoleUser),
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemInstruction, genai.RoleUser),
	}

	resp, err := a.client.Models.GenerateContent(timedCtx, modelName, contents, config)
	if err != nil {
		// Distinguish timeout from other API errors so the HTTP handler can
		// return 504 vs 422 as required by the design spec.
		if errors.Is(err, context.DeadlineExceeded) || timedCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", fmt.Errorf("%w: %v", ErrAPIFailure, err)
	}

	rawText := resp.Text()
	if rawText == "" {
		return "", fmt.Errorf("%w: model returned an empty response", ErrAPIFailure)
	}

	return rawText, nil
}
