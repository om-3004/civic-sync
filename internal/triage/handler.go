package triage

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

// triageRequest is the expected JSON body for POST /triage.
type triageRequest struct {
	ImageURL string `json:"image_url"`
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
}

// Handler holds the dependencies for the triage HTTP handler.
type Handler struct {
	agent *Agent
}

// NewHandler returns a Handler wired to the given triage Agent.
func NewHandler(agent *Agent) *Handler {
	return &Handler{agent: agent}
}

// TriageHandler handles POST /triage.
//
// It validates the request body, calls the triage agent, parses the AI
// response, and returns the structured TriageResult.
//
// Response codes:
//   - 200: triage succeeded → returns TriageResult JSON
//   - 400: missing or invalid image_url / location fields (Req 3.1)
//   - 422: Gemini returned an unparseable or invalid response (Req 3.2)
//   - 504: Gemini API call timed out (>10 s) (Req 3.5)
//
// The JWT middleware runs before this handler; no auth check is needed here.
func (h *Handler) TriageHandler(w http.ResponseWriter, r *http.Request) {
	// ── 1. Decode request body ────────────────────────────────────────────────
	var req triageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// ── 2. Validate image_url ─────────────────────────────────────────────────
	if strings.TrimSpace(req.ImageURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image_url is required"})
		return
	}

	// ── 3. Validate location ──────────────────────────────────────────────────
	// Both fields default to 0.0 on an absent or zero-value JSON object.
	// We reject the zero-zero coordinate (null island) as well as any
	// out-of-range value so clearly invalid payloads are caught early.
	lat := req.Location.Latitude
	lng := req.Location.Longitude

	if lat == 0 && lng == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "location with valid latitude and longitude is required"})
		return
	}
	if lat < -90 || lat > 90 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "location.latitude must be between -90 and 90"})
		return
	}
	if lng < -180 || lng > 180 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "location.longitude must be between -180 and 180"})
		return
	}

	// ── 4. Call the triage agent ──────────────────────────────────────────────
	raw, err := h.agent.Triage(r.Context(), req.ImageURL, lat, lng)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "triage request timed out"})
			return
		}
		// ErrAPIFailure or any other agent error → 422.
		log.Printf("triage agent error: %v", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "AI classification failed"})
		return
	}

	// ── 5. Parse and validate the Gemini response ─────────────────────────────
	result, err := parseGeminiResponse(raw)
	if err != nil {
		log.Printf("triage parse error: %v | raw: %s", err, raw)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "AI response could not be parsed"})
		return
	}

	// ── 6. Return success ─────────────────────────────────────────────────────
	writeJSON(w, http.StatusOK, result)
}

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
