package triage

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// markdownFenceRe matches optional markdown code fences that Gemini may
// accidentally wrap around its JSON output, e.g.:
//
//	```json
//	{ ... }
//	```
//
// or simply:
//
//	```
//	{ ... }
//	```
var markdownFenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*\\n?(.*?)\\n?```$")

// allowedCategories is the closed set of category values the AI may return.
// Any other value is rejected as per Req 3.7.
var allowedCategories = map[string]bool{
	"Pothole":           true,
	"Water Clogging":    true,
	"Drain Overflow":    true,
	"Electrical Hazard": true,
	"Street Light Out":  true,
	"Garbage Dumping":   true,
	"Broken Road":       true,
	"Tree Fallen":       true,
	"Sewage Overflow":   true,
	"Other":             true,
}

// Sentinel errors used by the HTTP handler to decide the response status.
var (
	// ErrUnparseable is returned when the raw string is not valid JSON.
	// HTTP handler should map this to HTTP 422.
	ErrUnparseable = errors.New("triage: gemini response is not valid JSON")

	// ErrUnknownCategory is returned when the category field does not match
	// any value in allowedCategories. HTTP handler should map this to HTTP 422.
	ErrUnknownCategory = errors.New("triage: gemini returned an unknown category")

	// ErrEmptyField is returned when one or more required fields are empty
	// after parsing. HTTP handler should map this to HTTP 422.
	ErrEmptyField = errors.New("triage: gemini response is missing required fields")
)

// parseGeminiResponse cleans a raw Gemini text response, unmarshals it into a
// TriageResult, validates the category, and enforces character-length limits on
// the title and description fields.
//
// Steps:
//  1. Strip accidental markdown code fences (```json … ``` or ``` … ```).
//  2. json.Unmarshal the cleaned string into TriageResult.
//  3. Return ErrUnparseable if unmarshal fails.
//  4. Return ErrEmptyField if category, title, or description are empty.
//  5. Return ErrUnknownCategory if category is not in allowedCategories.
//  6. Trim title to 100 runes; trim description to 500 runes.
func parseGeminiResponse(raw string) (*TriageResult, error) {
	// ── Step 1: strip markdown code fences ────────────────────────────────────
	cleaned := strings.TrimSpace(raw)
	if m := markdownFenceRe.FindStringSubmatch(cleaned); m != nil {
		cleaned = strings.TrimSpace(m[1])
	}

	// ── Step 2 & 3: unmarshal JSON ────────────────────────────────────────────
	var result TriageResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnparseable, err)
	}

	// ── Step 4: validate required fields are non-empty ────────────────────────
	if result.Category == "" || result.Title == "" || result.Description == "" {
		return nil, fmt.Errorf("%w: category=%q title=%q description=%q",
			ErrEmptyField, result.Category, result.Title, result.Description)
	}

	// ── Step 5: validate category against the allowed enum ────────────────────
	if !allowedCategories[result.Category] {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCategory, result.Category)
	}

	// ── Step 6: enforce character limits using rune slices (Unicode-safe) ─────
	if runes := []rune(result.Title); len(runes) > 100 {
		result.Title = string(runes[:100])
	}
	if runes := []rune(result.Description); len(runes) > 500 {
		result.Description = string(runes[:500])
	}

	return &result, nil
}
