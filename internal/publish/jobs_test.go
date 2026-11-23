package publish

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestManager_LifecycleAndPruning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "publish-jobs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	jobFile := filepath.Join(tmpDir, "jobs.json")
	var executedCount atomic.Int32

	mgr, err := NewManager(jobFile, func(ctx context.Context, step func(string)) error {
		executedCount.Add(1)
		step("verifying")
		return nil
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer mgr.Close()

	// Enqueue 65 jobs to trigger pruning (maxRetainedJobs = 50)
	for i := 0; i < 65; i++ {
		_, err := mgr.Enqueue()
		if err != nil {
			t.Fatalf("failed to enqueue job %d: %v", i, err)
		}
	}

	// Wait for worker to process queued jobs
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		latest, ok := mgr.Latest()
		if ok && (latest.Status == "succeeded" || latest.Status == "failed") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mgr.mu.Lock()
	jobCount := len(mgr.jobs)
	mgr.mu.Unlock()

	if jobCount > maxRetainedJobs {
		t.Fatalf("expected jobs count <= %d after pruning, got %d", maxRetainedJobs, jobCount)
	}

	// Verify Close cleans up goroutine
	mgr.Close()

	// Ensure idempotence of Close()
	mgr.Close()
}

func TestManager_RetryFailedJob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "publish-retry-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	jobFile := filepath.Join(tmpDir, "jobs.json")
	var shouldFail atomic.Bool
	shouldFail.Store(true)

	mgr, err := NewManager(jobFile, func(ctx context.Context, step func(string)) error {
		if shouldFail.Load() {
			return fmt.Errorf("simulated publication failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer mgr.Close()

	job, err := mgr.Enqueue()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for it to fail
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := mgr.Get(job.ID)
		if ok && j.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	j, ok := mgr.Get(job.ID)
	if !ok || j.Status != "failed" {
		t.Fatalf("expected job to be failed, got: %+v", j)
	}

	// Now allow success and retry
	shouldFail.Store(false)
	retryJob, err := mgr.Retry(job.ID)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if retryJob.Revision <= j.Revision {
		t.Fatalf("expected retry job revision to be greater than %d, got %d", j.Revision, retryJob.Revision)
	}

	// Wait for retry job to complete
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rj, ok := mgr.Get(retryJob.ID)
		if ok && rj.Status == "succeeded" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	rj, ok := mgr.Get(retryJob.ID)
	if !ok || rj.Status != "succeeded" {
		t.Fatalf("expected retried job to succeed, got: %+v", rj)
	}
}

func TestManager_CloseCancelsInFlightPublish(t *testing.T) {
	tmpDir := t.TempDir()
	jobFile := filepath.Join(tmpDir, "jobs.json")

	publishStarted := make(chan struct{})
	publishCanceled := make(chan struct{})

	mgr, err := NewManager(jobFile, func(ctx context.Context, step func(string)) error {
		close(publishStarted)
		select {
		case <-ctx.Done():
			close(publishCanceled)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	_, err = mgr.Enqueue()
	if err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	<-publishStarted
	mgr.Close()

	select {
	case <-publishCanceled:
		// Success! Close caused context cancellation
	case <-time.After(2 * time.Second):
		t.Fatalf("expected publish context to be canceled upon manager Close")
	}
}
