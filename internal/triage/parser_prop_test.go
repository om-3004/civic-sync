// Feature: civic-sync, Property 3: AI Triage Response Parsing Always Produces a Valid Category

// **Validates: Requirements 3.2, 3.7**
package triage

import (
	"encoding/json"
	"errors"
	"testing"

	"pgregory.net/rapid"
)

// allowed is the closed set of valid categories, mirroring allowedCategories in parser.go.
var allowed = []string{
	"Pothole",
	"Water Clogging",
	"Drain Overflow",
	"Electrical Hazard",
	"Other",
}

// isAllowed returns true if s is a member of the allowed set.
func isAllowed(s string) bool {
	for _, c := range allowed {
		if s == c {
			return true
		}
	}
	return false
}

// validCategoryGen draws a category from the allowed set.
var validCategoryGen = rapid.SampledFrom(allowed)

// buildValidJSON serialises a TriageResult-shaped map into a JSON string.
func buildValidJSON(category, title, description string) string {
	b, _ := json.Marshal(map[string]string{
		"category":    category,
		"title":       title,
		"description": description,
	})
	return string(b)
}

// TestParseGeminiResponse_ValidCategoryInput checks that, for any valid
// category drawn from the allowed set (with non-empty title and description),
// parseGeminiResponse succeeds and the returned Category is in the allowed set.
func TestParseGeminiResponse_ValidCategoryInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		category := validCategoryGen.Draw(t, "category")
		title := rapid.StringN(1, 100, -1).Draw(t, "title")
		description := rapid.StringN(1, 500, -1).Draw(t, "description")

		input := buildValidJSON(category, title, description)

		result, err := parseGeminiResponse(input)
		if err != nil {
			t.Fatalf("expected no error for valid input, got: %v (input=%q)", err, input)
		}
		if result == nil {
			t.Fatalf("expected non-nil result, got nil (input=%q)", input)
		}
		if !isAllowed(result.Category) {
			t.Fatalf("result.Category %q is not in the allowed set (input=%q)", result.Category, input)
		}
	})
}

// TestParseGeminiResponse_MalformedInput checks that, for any random / non-JSON
// string, parseGeminiResponse always returns an error (never a result with an
// invalid category).
func TestParseGeminiResponse_MalformedInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Draw strings that are NOT valid JSON objects: rapid.String() produces
		// arbitrary Unicode strings; the vast majority won't parse as JSON.
		input := rapid.String().Draw(t, "malformed")

		result, err := parseGeminiResponse(input)
		if err != nil {
			// Expected path: parser correctly rejected the malformed input.
			if !errors.Is(err, ErrUnparseable) &&
				!errors.Is(err, ErrEmptyField) &&
				!errors.Is(err, ErrUnknownCategory) {
				t.Fatalf("unexpected error type %v for input %q", err, input)
			}
			return
		}

		// If no error was returned the result MUST carry a valid category.
		if result == nil || !isAllowed(result.Category) {
			t.Fatalf("non-error result has invalid/empty category %q for input %q",
				func() string {
					if result != nil {
						return result.Category
					}
					return "<nil>"
				}(), input)
		}
	})
}

// TestParseGeminiResponse_RandomCategoryInput checks that, for valid JSON where
// the category field is a random string (not necessarily in the allowed set),
// parseGeminiResponse either (a) returns a non-error result with an allowed
// category, or (b) returns an error — it MUST NOT return a non-error result
// with an invalid/empty category.
func TestParseGeminiResponse_RandomCategoryInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Random category: occasionally this will coincidentally be valid.
		category := rapid.String().Draw(t, "random_category")
		// Use non-empty title and description so the only rejection trigger is
		// the category itself.
		title := rapid.StringN(1, 100, -1).Draw(t, "title")
		description := rapid.StringN(1, 500, -1).Draw(t, "description")

		input := buildValidJSON(category, title, description)

		result, err := parseGeminiResponse(input)
		if err != nil {
			// Accepted: parser correctly rejected the unknown/empty category.
			if !errors.Is(err, ErrUnparseable) &&
				!errors.Is(err, ErrEmptyField) &&
				!errors.Is(err, ErrUnknownCategory) {
				t.Fatalf("unexpected error type %v for input %q", err, input)
			}
			return
		}

		// Non-error path: the category MUST be in the allowed set.
		if result == nil || !isAllowed(result.Category) {
			t.Fatalf("non-error result has invalid/empty category %q for input %q",
				func() string {
					if result != nil {
						return result.Category
					}
					return "<nil>"
				}(), input)
		}
	})
}
