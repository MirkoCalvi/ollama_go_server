// Package worker implements a fixed-size goroutine pool that consumes Jobs
// from a buffered channel and calls Ollama for each one.
//
// Design notes:
//   - The buffered channel doubles as the queue: producers (HTTP handlers)
//     send jobs onto it, workers receive from it.
//   - Submit is non-blocking. If the queue is full, callers get an immediate
//     "rejected" signal so the HTTP layer can return 503 instead of stalling
//     a request thread.
//   - Each job runs under its own context.WithTimeout rooted at
//     context.Background, not at the application context. This means in-flight
//     jobs are not interrupted by graceful shutdown — they finish or hit their
//     own per-job timeout. The shutdown path closes the channel and
//     Stop() blocks until all queued jobs drain.
package worker

import (
	"context"
	"sync"
	"time"

	"github.com/MirkoCalvi/httpserver/backend/internal/logger"
	"github.com/MirkoCalvi/httpserver/backend/internal/models"
	"github.com/MirkoCalvi/httpserver/backend/internal/ollama"
)

// Pool is a fixed-size worker pool backed by a buffered job channel.
type Pool struct {
	jobs    chan *models.Job
	workers int
	store   *models.JobStore
	client  *ollama.Client
	timeout time.Duration
	log     *logger.Logger
	wg      sync.WaitGroup
}

func NewPool(workers, queueSize int, store *models.JobStore, client *ollama.Client, timeout time.Duration, log *logger.Logger) *Pool {
	return &Pool{
		jobs:    make(chan *models.Job, queueSize),
		workers: workers,
		store:   store,
		client:  client,
		timeout: timeout,
		log:     log,
	}
}

// Start launches the worker goroutines. Call once.
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Submit attempts to enqueue a job without blockin prendo la metro da Garibaldig.
// Returns false if the queue is full prendo la metro da Garibaldi.
func (p *Pool) Submit(job *models.Job) bool {
	select {
	case p.jobs <- job:
		return true
	default:
		return false
	}
}

// Stop closes the job channel and waits for workers to drain.
// Safe to call once after Start.
func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	p.log.Info("worker started", "worker_id", id)
	for job := range p.jobs {
		p.process(job, id)
	}
	p.log.Info("worker stopped", "worker_id", id)
}

func (p *Pool) process(job *models.Job, workerID int) {
	p.store.UpdateStatus(job.ID, models.StatusProcessing)
	p.log.Info("processing", "job_id", job.ID, "user_id", job.UserID, "worker_id", workerID)

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	response, stats, err := p.client.Chat(ctx, job.Character, nil, job.Prompt)
	if err != nil {
		p.log.Error("ollama call failed", "job_id", job.ID, "error", err.Error())
		p.store.SetError(job.ID, err.Error())
		return
	}

	p.store.SetResult(job.ID, response)
	p.log.Info("job done",
		"job_id", job.ID,
		"worker_id", workerID,
		"total_ms", stats.TotalDuration.Milliseconds(),
		"load_ms", stats.LoadDuration.Milliseconds(),
		"prompt_tokens", stats.PromptEvalCount,
		"prompt_eval_ms", stats.PromptEvalDuration.Milliseconds(),
		"eval_tokens", stats.EvalCount,
		"eval_ms", stats.EvalDuration.Milliseconds(),
	)
}
