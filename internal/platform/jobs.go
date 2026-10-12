package platform

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

type JobRunner struct {
	mu   sync.RWMutex
	jobs map[string]*BackgroundJob
}

func NewJobRunner() *JobRunner {
	return &JobRunner{jobs: map[string]*BackgroundJob{}}
}

func (r *JobRunner) Start(ctx context.Context, prompt string, work func(context.Context, func(string)) (string, error)) (BackgroundJob, error) {
	if r == nil {
		return BackgroundJob{}, errors.New("job runner is nil")
	}
	if ctx == nil {
		return BackgroundJob{}, errors.New("job context is required")
	}
	if work == nil {
		return BackgroundJob{}, errors.New("job work is required")
	}
	job := &BackgroundJob{
		ID:        uuid.NewString(),
		Prompt:    prompt,
		Status:    JobQueued,
		StartedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.jobs[job.ID] = job
	snapshot := cloneJob(job)
	r.mu.Unlock()

	go func() {
		r.mu.Lock()
		job.Status = JobRunning
		r.mu.Unlock()

		result, err := work(ctx, func(message string) {
			r.mu.Lock()
			job.Progress = append(job.Progress, message)
			r.mu.Unlock()
		})
		now := time.Now().UTC()

		r.mu.Lock()
		defer r.mu.Unlock()
		job.FinishedAt = &now
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				job.Status = JobCancelled
			} else {
				job.Status = JobFailed
				job.Error = err.Error()
			}
			return
		}
		if ctx.Err() != nil {
			job.Status = JobCancelled
			return
		}
		job.Status = JobSucceeded
		job.Result = result
	}()

	return snapshot, nil
}

func (r *JobRunner) Get(id string) (BackgroundJob, bool) {
	if r == nil {
		return BackgroundJob{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	if !ok {
		return BackgroundJob{}, false
	}
	return cloneJob(job), true
}

func (r *JobRunner) List() []BackgroundJob {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BackgroundJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, cloneJob(job))
	}
	return out
}

func cloneJob(job *BackgroundJob) BackgroundJob {
	copy := *job
	copy.Progress = append([]string(nil), job.Progress...)
	if job.FinishedAt != nil {
		finished := *job.FinishedAt
		copy.FinishedAt = &finished
	}
	return copy
}
