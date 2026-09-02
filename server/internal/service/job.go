package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	JobTTL           = 30 * time.Minute
	JobTimeout       = 2 * time.Hour
	SSEHeartbeatEvery = 15 * time.Second
)

// JobProgress 异步任务进度（SSE/轮询 JSON 形状：migrated）。
type JobProgress struct {
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Migrated int    `json:"migrated"`
	Failed   int    `json:"failed"`
	Key      string `json:"key,omitempty"`
	Error    string `json:"error,omitempty"`
	Status   string `json:"status,omitempty"`
}

// JobResult 异步任务终态汇总。
type JobResult struct {
	Migrated  int
	Failed    int
	LastError string
	FailKeys  []string
}

// Job 单次异步批量任务。
type Job struct {
	ID      string
	Created time.Time
	Total   int

	mu       sync.Mutex
	progress JobProgress
	result   JobResult
	done     bool
	cancel   context.CancelFunc
	subs     map[chan JobProgress]struct{}
}

// JobRegistry 内存任务注册表（reap + 关停取消）。
type JobRegistry struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	stopCh chan struct{}
	once   sync.Once
}

// NewJobRegistry 创建并启动 reap 循环。
func NewJobRegistry() *JobRegistry {
	r := &JobRegistry{
		jobs:   make(map[string]*Job),
		stopCh: make(chan struct{}),
	}
	go r.reapLoop()
	return r
}

// Stop 取消未完成任务并停止 reap。
func (r *JobRegistry) Stop() {
	r.once.Do(func() { close(r.stopCh) })
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		j.mu.Lock()
		if j.cancel != nil && !j.done {
			j.cancel()
		}
		j.mu.Unlock()
	}
}

func (r *JobRegistry) reapLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			r.Reap()
		case <-r.stopCh:
			return
		}
	}
}

// Reap 清理过期已完成任务（测试可调用）。
func (r *JobRegistry) Reap() {
	cutoff := time.Now().Add(-JobTTL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, j := range r.jobs {
		j.mu.Lock()
		expired := j.done && j.Created.Before(cutoff)
		j.mu.Unlock()
		if expired {
			delete(r.jobs, id)
		}
	}
}

// Create 注册新任务。
func (r *JobRegistry) Create(total int, cancel context.CancelFunc) *Job {
	j := &Job{
		ID:       uuid.NewString(),
		Created:  time.Now(),
		Total:    total,
		progress: JobProgress{Total: total, Status: "running"},
		subs:     make(map[chan JobProgress]struct{}),
		cancel:   cancel,
	}
	r.mu.Lock()
	r.jobs[j.ID] = j
	r.mu.Unlock()
	return j
}

// Get 按 id 取任务。
func (r *JobRegistry) Get(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	return j, ok
}

// Subscribe 订阅进度；已结束则立即推送终态并关闭 channel。
func (j *Job) Subscribe() chan JobProgress {
	ch := make(chan JobProgress, 16)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		ch <- j.progress
		close(ch)
		return ch
	}
	j.subs[ch] = struct{}{}
	ch <- j.progress
	return ch
}

// Unsubscribe 取消订阅。
func (j *Job) Unsubscribe(ch chan JobProgress) {
	j.mu.Lock()
	delete(j.subs, ch)
	j.mu.Unlock()
}

// Emit 广播中间进度（慢订阅者可丢中间帧）。
func (j *Job) Emit(p JobProgress) {
	j.mu.Lock()
	j.progress = p
	subs := make([]chan JobProgress, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- p:
		default:
		}
	}
}

// Finish 写入终态并阻塞投递（短超时）后关闭订阅。
func (j *Job) Finish(out JobResult, status string) {
	j.mu.Lock()
	if j.done {
		j.mu.Unlock()
		return
	}
	j.result = out
	j.done = true
	if status == "" {
		status = "done"
	}
	j.progress = JobProgress{
		Done: j.Total, Total: j.Total,
		Migrated: out.Migrated, Failed: out.Failed, Status: status,
	}
	subs := make([]chan JobProgress, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	final := j.progress
	j.subs = make(map[chan JobProgress]struct{})
	j.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- final:
		case <-time.After(5 * time.Second):
		}
		close(ch)
	}
}

// Snapshot 返回当前进度/结果/是否完成。
func (j *Job) Snapshot() (JobProgress, JobResult, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.progress, j.result, j.done
}

// Cancel 取消未完成任务；已完成返回 false。
func (j *Job) Cancel() (cancelled bool, alreadyDone bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return false, true
	}
	if j.cancel != nil {
		j.cancel()
	}
	return true, false
}

// ProgressFrom 将批量 Progress 转为 JobProgress（OK→migrated）。
func ProgressFrom(p Progress) JobProgress {
	return JobProgress{
		Done: p.Done, Total: p.Total, Migrated: p.OK, Failed: p.Failed,
		Key: p.Key, Error: p.Error, Status: p.Status,
	}
}

// ResultFromBatch 将 BatchResult 转为 JobResult。
func ResultFromBatch(out BatchResult) JobResult {
	return JobResult{
		Migrated: out.OK, Failed: out.Failed, LastError: out.LastError, FailKeys: out.FailKeys,
	}
}
