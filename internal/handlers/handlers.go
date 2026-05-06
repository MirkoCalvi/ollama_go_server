// Package handlers contains the HTTP handlers for the public API.
//
// Routes:
//   - POST /generate     — accept a prompt, enqueue a job, return its ID.
//   - GET  /jobs/{id}    — return the current state of a job.
//
// The mux registered in main.go uses plain path-prefix routing for /jobs/
// (since this project targets Go 1.18, before pattern-based ServeMux). The
// {id} segment is parsed inside GetJob.
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MirkoCalvi/httpserver/internal/logger"
	"github.com/MirkoCalvi/httpserver/internal/models"
	"github.com/MirkoCalvi/httpserver/internal/worker"
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
type generateRequest struct {
	UserID string `json:"user_id"`
	Prompt string `json:"prompt"`
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
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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

	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "user_id and prompt are required")
		return
	}

	now := time.Now()
	job := &models.Job{
		ID:        uuid.NewString(),
		UserID:    req.UserID,
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
	writeJSON(w, http.StatusAccepted, jobResponse{
		JobID:  job.ID,
		Status: job.Status,
	})
}

// GetJob handles GET /jobs/{id}. Returns 404 if the ID is unknown.
//
// Path parsing is manual: the mux is registered for the prefix "/jobs/", so
// anything after that prefix is treated as the ID. Empty IDs and IDs with
// further path segments are rejected.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	job, ok := h.store.Get(id)
	if !ok {
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
