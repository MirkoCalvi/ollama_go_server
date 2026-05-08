// Package handlers contains the HTTP handlers for the public API.
//
// Routes:
//   - POST /generate     — accept a prompt, enqueue a job, return its ID.
//   - GET  /jobs/{id}    — return the current state of a job.
//
// Both routes are protected by auth middleware in main.go: the handlers
// expect the authenticated user_id to be present on r.Context() (via
// auth.UserFrom). The request body for /generate carries only the prompt;
// the job's UserID is taken from the authenticated principal, never from
// the client.
//
// The mux uses plain path-prefix routing for /jobs/ (since this project
// targets Go 1.18, before pattern-based ServeMux). The {id} segment is
// parsed inside GetJob.
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MirkoCalvi/httpserver/backend/internal/auth"
	"github.com/MirkoCalvi/httpserver/backend/internal/logger"
	"github.com/MirkoCalvi/httpserver/backend/internal/models"
	"github.com/MirkoCalvi/httpserver/backend/internal/ollama/personalities"
	"github.com/MirkoCalvi/httpserver/backend/internal/worker"
)

// Cap request bodies. Prompts in production can be large but should not be
// "upload a movie" large. 1 MiB is generous for text and protects against
// memory abuse.
const maxBodyBytes = 1 << 20 // 1 MiB

// Handler bundles the dependencies needed by every HTTP route.
type Handler struct {
	store *models.JobStore
	pool  *worker.Pool
	log   *logger.Logger
}

func New(store *models.JobStore, pool *worker.Pool, log *logger.Logger) *Handler {
	return &Handler{store: store, pool: pool, log: log}
}

// generateRequest is the body shape accepted by POST /generate.
//
// Note: there is no user_id field. The authenticated user comes from the
// Authorization header via auth middleware. DisallowUnknownFields will
// reject any client that still sends user_id in the body.
type generateRequest struct {
	Prompt string `json:"prompt"`
	Character string `json:"character"`
}

// jobResponse is the body shape returned by both /generate and /jobs/{id}.
//
// `response` and `error` are omitempty: a queued or processing job has no
// response yet, and a successful job has no error.
type jobResponse struct {
	JobID    string           `json:"job_id"`
	Status   models.JobStatus `json:"status"`
	Response string           `json:"response,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// Generate handles POST /generate. Validates input, creates a Job in the
// store with status=queued, hands it to the worker pool, and returns 202
// Accepted with the job_id. The actual Ollama call happens asynchronously.
//
// The job's UserID is the authenticated principal from r.Context(); the
// body cannot override it.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := auth.UserFrom(r.Context())
	if userID == "" {
		// Auth middleware should have rejected before reaching here. If it
		// didn't, something is wired wrong — fail closed rather than
		// creating an unowned job.
		writeError(w, http.StatusInternalServerError, "missing authenticated user")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req generateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	if strings.TrimSpace(req.Character) == "" {
		writeError(w, http.StatusBadRequest, "Character is required")
		return
	}

	newCharacter := personalities.Get(req.Character)
	if newCharacter == nil {
		writeError(w, http.StatusBadRequest, "invalid Character type")
		return
	}

	now := time.Now()
	job := &models.Job{
		ID:        uuid.NewString(),
		UserID:    userID,
		Character: newCharacter,
		Prompt:    req.Prompt,
		Status:    models.StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	h.store.Create(job)

	if !h.pool.Submit(job) {
		// Queue is full: mark the job as failed so a later GET /jobs/{id}
		// reflects reality, and tell the caller to retry.
		h.store.SetError(job.ID, "queue full")
		writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again later")
		return
	}

	h.log.Info("job queued", "job_id", job.ID, "user_id", job.UserID)
	// Always report "queued" here. Reading job.Status after Submit would
	// race with a worker that has already picked the job up and called
	// store.UpdateStatus on the same *Job.
	writeJSON(w, http.StatusAccepted, jobResponse{
		JobID:  job.ID,
		Status: models.StatusQueued,
	})
}

// Characters handles GET /characters. Returns the list of character names a
// client may pass as the "character" field on POST /generate. Unauthenticated
// — the list is not sensitive and a frontend needs it on the login screen
// before a Firebase token is available.
func (h *Handler) Characters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, personalities.Names())
}

// GetJob handles GET /jobs/{id}. Returns 404 if the ID is unknown OR if the
// job belongs to a different user — the two cases are intentionally
// indistinguishable so non-owners can't probe which job IDs exist.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := auth.UserFrom(r.Context())
	if userID == "" {
		writeError(w, http.StatusInternalServerError, "missing authenticated user")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	job, ok := h.store.Get(id)
	if !ok || job.UserID != userID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, jobResponse{
		JobID:    job.ID,
		Status:   job.Status,
		Response: job.Response,
		Error:    job.Error,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
