package service

import (
	"context"
	"testing"
	"time"
)

func TestJobRegistryLifecycle(t *testing.T) {
	r := NewJobRegistry()
	defer r.Stop()
	_, cancel := context.WithCancel(context.Background())
	job := r.Create(2, cancel)
	if job.ID == "" || job.Total != 2 {
		t.Fatalf("job=%+v", job)
	}
	got, ok := r.Get(job.ID)
	if !ok || got != job {
		t.Fatal("get")
	}
	ch := job.Subscribe()
	job.Emit(JobProgress{Done: 1, Total: 2, Migrated: 1, Status: "running"})
	select {
	case p := <-ch:
		if p.Migrated != 0 && p.Migrated != 1 {
			t.Fatalf("unexpected migrated=%d", p.Migrated)
		}
	case <-time.After(time.Second):
		t.Fatal("no progress")
	}
	// drain
	for {
		select {
		case <-ch:
		default:
			goto done
		}
	}
done:
	job.Finish(JobResult{Migrated: 2}, "done")
	select {
	case p, open := <-ch:
		if open && p.Status != "done" {
			t.Fatalf("final=%+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no final")
	}
	cancelled, done := job.Cancel()
	if cancelled || !done {
		t.Fatalf("cancel after done: %v %v", cancelled, done)
	}
}

func TestProgressFrom(t *testing.T) {
	p := ProgressFrom(Progress{Done: 1, Total: 2, OK: 1, Failed: 0, Status: "running"})
	if p.Migrated != 1 || p.Done != 1 {
		t.Fatalf("%+v", p)
	}
}
