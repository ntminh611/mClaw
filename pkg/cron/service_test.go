package cron

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddAndListJobs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	cs := NewCronService(storePath, nil)

	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}

	job, err := cs.AddJob("test-job", schedule, "hello", true, "telegram", "123")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if job.Name != "test-job" {
		t.Errorf("expected name 'test-job', got '%s'", job.Name)
	}
	if !job.Enabled {
		t.Error("job should be enabled")
	}
	if job.Payload.Message != "hello" {
		t.Errorf("expected message 'hello', got '%s'", job.Payload.Message)
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	// Verify persistence
	cs2 := NewCronService(storePath, nil)
	jobs2 := cs2.ListJobs(true)
	if len(jobs2) != 1 {
		t.Fatalf("expected 1 job after reload, got %d", len(jobs2))
	}
}

func TestRemoveJob(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	cs := NewCronService(storePath, nil)

	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	job, _ := cs.AddJob("remove-me", schedule, "test", false, "", "")

	if !cs.RemoveJob(job.ID) {
		t.Error("RemoveJob should return true")
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", len(jobs))
	}

	// Remove non-existent
	if cs.RemoveJob("nonexistent") {
		t.Error("RemoveJob should return false for nonexistent ID")
	}
}

func TestEnableDisableJob(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	cs := NewCronService(storePath, nil)

	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	job, _ := cs.AddJob("toggle-me", schedule, "test", false, "", "")

	// Disable
	result := cs.EnableJob(job.ID, false)
	if result == nil {
		t.Fatal("EnableJob returned nil")
	}
	if result.Enabled {
		t.Error("job should be disabled")
	}

	// Verify disabled jobs not in enabled-only list
	enabledOnly := cs.ListJobs(false)
	if len(enabledOnly) != 0 {
		t.Errorf("expected 0 enabled jobs, got %d", len(enabledOnly))
	}

	// Re-enable
	result = cs.EnableJob(job.ID, true)
	if result == nil || !result.Enabled {
		t.Error("job should be re-enabled")
	}
}

func TestAtScheduleDisablesAfterRun(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	var handlerCalled atomic.Bool
	handler := func(job *CronJob) (string, error) {
		handlerCalled.Store(true)
		return "done", nil
	}

	cs := NewCronService(storePath, handler)

	// Schedule "at" in the future first so AddJob sets NextRunAtMS
	futureMS := time.Now().Add(1 * time.Hour).UnixMilli()
	schedule := CronSchedule{Kind: "at", AtMS: &futureMS}
	job, _ := cs.AddJob("one-shot", schedule, "do this once", false, "", "")

	// Now override NextRunAtMS to the past so it's immediately due
	pastMS := time.Now().Add(-1 * time.Second).UnixMilli()
	cs.mu.Lock()
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			cs.store.Jobs[i].State.NextRunAtMS = &pastMS
		}
	}
	cs.mu.Unlock()

	// Manually trigger checkJobs
	cs.running = true
	cs.checkJobs()

	// Wait for goroutine to complete
	time.Sleep(200 * time.Millisecond)

	if !handlerCalled.Load() {
		t.Error("handler should have been called")
	}

	// Verify job is now disabled
	cs.mu.RLock()
	for _, j := range cs.store.Jobs {
		if j.ID == job.ID {
			if j.Enabled {
				t.Error("at-schedule job should be disabled after execution")
			}
			if j.State.NextRunAtMS != nil {
				t.Error("at-schedule job should have nil NextRunAtMS after execution")
			}
		}
	}
	cs.mu.RUnlock()
}

func TestNoDuplicateExecution(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	var executionCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)

	handler := func(job *CronJob) (string, error) {
		executionCount.Add(1)
		// Simulate slow handler (LLM call)
		time.Sleep(500 * time.Millisecond)
		wg.Done()
		return "done", nil
	}

	cs := NewCronService(storePath, handler)

	// Add a job that's due now
	pastMS := time.Now().Add(-1 * time.Second).UnixMilli()
	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	job, _ := cs.AddJob("no-dupe", schedule, "test", false, "", "")

	// Manually set nextRunAtMS to the past
	cs.mu.Lock()
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			cs.store.Jobs[i].State.NextRunAtMS = &pastMS
		}
	}
	cs.mu.Unlock()

	cs.running = true

	// Call checkJobs multiple times rapidly (simulating 1s ticker)
	cs.checkJobs()
	cs.checkJobs()
	cs.checkJobs()

	// Wait for the single handler to finish
	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	count := executionCount.Load()
	if count != 1 {
		t.Errorf("expected exactly 1 execution, got %d (duplicate execution bug!)", count)
	}
}

func TestEveryScheduleReschedules(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	handler := func(job *CronJob) (string, error) {
		return "done", nil
	}

	cs := NewCronService(storePath, handler)

	everyMS := int64(5000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	job, _ := cs.AddJob("recurring", schedule, "test", false, "", "")

	// Set to past so it's due
	pastMS := time.Now().Add(-1 * time.Second).UnixMilli()
	cs.mu.Lock()
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			cs.store.Jobs[i].State.NextRunAtMS = &pastMS
		}
	}
	cs.mu.Unlock()

	cs.running = true
	cs.checkJobs()

	// Wait for execution
	time.Sleep(200 * time.Millisecond)

	// Verify nextRunAtMS is set to future
	cs.mu.RLock()
	for _, j := range cs.store.Jobs {
		if j.ID == job.ID {
			if j.State.NextRunAtMS == nil {
				t.Error("every-schedule job should have NextRunAtMS after execution")
			} else if *j.State.NextRunAtMS <= time.Now().UnixMilli() {
				t.Error("NextRunAtMS should be in the future")
			}
		}
	}
	cs.mu.RUnlock()
}

func TestJobRemovedDuringExecution(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	removeCh := make(chan struct{})
	handler := func(job *CronJob) (string, error) {
		// Signal that handler started, then wait
		close(removeCh)
		time.Sleep(200 * time.Millisecond)
		return "done", nil
	}

	cs := NewCronService(storePath, handler)

	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	job, _ := cs.AddJob("will-be-removed", schedule, "test", false, "", "")

	pastMS := time.Now().Add(-1 * time.Second).UnixMilli()
	cs.mu.Lock()
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			cs.store.Jobs[i].State.NextRunAtMS = &pastMS
		}
	}
	cs.mu.Unlock()

	cs.running = true
	cs.checkJobs()

	// Wait for handler to start, then remove the job
	<-removeCh
	cs.RemoveJob(job.ID)

	// Wait for execution to complete — should not panic
	time.Sleep(500 * time.Millisecond)

	jobs := cs.ListJobs(true)
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestCronStoreFileCreated(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sub", "jobs.json")

	cs := NewCronService(storePath, nil)

	everyMS := int64(60000)
	schedule := CronSchedule{Kind: "every", EveryMS: &everyMS}
	_, err := cs.AddJob("test", schedule, "msg", false, "", "")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("store file should exist: %v", err)
	}
}
