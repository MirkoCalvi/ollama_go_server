// Package models defines the core domain types and the in-memory, thread-safe
// job store used to track work in flight.
package models

import (
	"sync"
	"time"
	"github.com/MirkoCalvi/httpserver/backend/internal/ollama"
)

// JobStatus represents the lifecycle stage of a Job.
//
// The public API contract documents queued|processing|done. "failed" is used
// internally so callers can distinguish a job whose Ollama call returned an
// error from one that is still pending — without it, a failed job would look
// indefinitely stuck in "processing".
type JobStatus string

const (
	StatusQueued     JobStatus = "queued"
	StatusProcessing JobStatus = "processing"
	StatusDone       JobStatus = "done"
	StatusFailed     JobStatus = "failed"
)

// Job is the unit of work enqueued by /generate and consumed by the worker pool.
type Job struct {
	ID        string
	UserID    string
	Character *ollama.Character
	Prompt    string
	Status    JobStatus
	Response  string
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// JobResult is the value a worker produces after calling Ollama. Kept as a
// distinct type to decouple "what the worker emits" from "what the store holds".
type JobResult struct {
	JobID    string
	UserID   string
	Response string
	Err      error
}

// JobStore is a concurrency-safe in-memory map of jobs keyed by Job.ID.
//
// Get returns a value copy of the Job so callers cannot accidentally mutate
// shared state without going through the store's setters.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewJobStore() *JobStore {
	return &JobStore{jobs: make(map[string]*Job)}
}

// Create inserts a new job. Existing jobs with the same ID are overwritten;
// IDs are UUIDs so collisions are not a real concern.
func (s *JobStore) Create(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

// Get returns a copy of the job and a found flag.
func (s *JobStore) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

func (s *JobStore) UpdateStatus(id string, status JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = status
		j.UpdatedAt = time.Now()
	}
}

func (s *JobStore) SetResult(id, response string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = StatusDone
		j.Response = response
		j.UpdatedAt = time.Now()
	}
}

func (s *JobStore) SetError(id, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = StatusFailed
		j.Error = errMsg
		j.UpdatedAt = time.Now()
	}
}
