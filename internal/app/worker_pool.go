package app

import (
	"context"
	"errors"
	"sync/atomic"
)

var errQueueFull = errors.New("worker queue is full")

type workerJob struct {
	ctx  context.Context
	run  func() error
	done chan error
}

type WorkerPool struct {
	jobs      chan workerJob
	workers   int
	completed atomic.Int64
	failed    atomic.Int64
	active    atomic.Int64
	rejected  atomic.Int64
}

func NewWorkerPool(workers, queue int) *WorkerPool {
	if workers <= 0 {
		workers = 2000
	}
	if queue <= 0 {
		queue = 50000
	}
	p := &WorkerPool{jobs: make(chan workerJob, queue), workers: workers}
	for i := 0; i < workers; i++ {
		go p.worker()
	}
	return p
}

func (p *WorkerPool) Run(ctx context.Context, fn func() error) error {
	job := workerJob{ctx: ctx, run: fn, done: make(chan error, 1)}
	select {
	case p.jobs <- job:
	default:
		p.rejected.Add(1)
		return errQueueFull
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *WorkerPool) worker() {
	for job := range p.jobs {
		if job.ctx.Err() != nil {
			job.done <- job.ctx.Err()
			continue
		}
		p.active.Add(1)
		err := job.run()
		p.active.Add(-1)
		if err != nil {
			p.failed.Add(1)
		} else {
			p.completed.Add(1)
		}
		job.done <- err
	}
}

func (p *WorkerPool) Stats() map[string]any {
	return map[string]any{
		"workers":   p.workers,
		"queue":     cap(p.jobs),
		"queued":    len(p.jobs),
		"active":    p.active.Load(),
		"completed": p.completed.Load(),
		"failed":    p.failed.Load(),
		"rejected":  p.rejected.Load(),
	}
}
