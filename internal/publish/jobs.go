package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const maxRetainedJobs = 50

type Job struct {
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Status    string    `json:"status"` // queued|running|verifying|succeeded|failed|superseded
	Attempts  int       `json:"attempts"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Manager serializes publications, stores jobs on disk and resumes queued work after restart.
// Revisions are coalesced: only the newest desired state is applied to Sub-Store.
type Manager struct {
	mu       sync.Mutex
	file     string
	jobs     []Job
	wake     chan struct{}
	stopChan chan struct{}
	stopOnce sync.Once
	publish  func(context.Context, func(string)) error
}

func NewManager(file string, fn func(context.Context, func(string)) error) (*Manager, error) {
	m := &Manager{
		file:     file,
		wake:     make(chan struct{}, 1),
		stopChan: make(chan struct{}),
		publish:  fn,
		jobs:     []Job{},
	}
	b, err := os.ReadFile(file)
	if err == nil {
		if err = json.Unmarshal(b, &m.jobs); err != nil {
			return nil, fmt.Errorf("decode publish jobs: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for i := range m.jobs {
		if m.jobs[i].Status == "running" || m.jobs[i].Status == "verifying" {
			m.jobs[i].Status = "queued"
		}
	}
	m.pruneOldJobsLocked()
	if err = m.persistLocked(); err != nil {
		return nil, err
	}
	go m.run()
	m.notify()
	return m, nil
}

func (m *Manager) Close() {
	m.stopOnce.Do(func() {
		close(m.stopChan)
	})
}

func (m *Manager) pruneOldJobsLocked() {
	if len(m.jobs) <= maxRetainedJobs {
		return
	}
	var active []Job
	var terminal []Job
	for _, j := range m.jobs {
		if j.Status == "queued" || j.Status == "running" || j.Status == "verifying" {
			active = append(active, j)
		} else {
			terminal = append(terminal, j)
		}
	}
	keepTerminal := maxRetainedJobs - len(active)
	if keepTerminal < 10 {
		keepTerminal = 10
	}
	if len(terminal) > keepTerminal {
		terminal = terminal[len(terminal)-keepTerminal:]
	}
	m.jobs = append(active, terminal...)
	sort.Slice(m.jobs, func(i, j int) bool {
		return m.jobs[i].Revision < m.jobs[j].Revision
	})
}
func (m *Manager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) persistLocked() error {
	b, err := json.MarshalIndent(m.jobs, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.file), ".jobs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.file)
}
func (m *Manager) Enqueue() (Job, error) {
	m.mu.Lock()
	var revision int64
	for i := range m.jobs {
		if m.jobs[i].Revision > revision {
			revision = m.jobs[i].Revision
		}
	}
	now := time.Now().UTC()
	j := Job{ID: uuid.NewString(), Revision: revision + 1, Status: "queued", CreatedAt: now, UpdatedAt: now}
	m.jobs = append(m.jobs, j)
	m.pruneOldJobsLocked()
	err := m.persistLocked()
	m.mu.Unlock()
	if err != nil {
		return Job{}, err
	}
	m.notify()
	return j, nil
}
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}
func (m *Manager) Latest() (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.jobs) == 0 {
		return Job{}, false
	}
	return m.jobs[len(m.jobs)-1], true
}
func (m *Manager) set(id, status, msg string, attempts int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.jobs {
		if m.jobs[i].ID == id {
			m.jobs[i].Status = status
			m.jobs[i].UpdatedAt = time.Now().UTC()
			m.jobs[i].Error = msg
			if attempts > 0 {
				m.jobs[i].Attempts = attempts
			}
			break
		}
	}
	m.pruneOldJobsLocked()
	_ = m.persistLocked()
}
func (m *Manager) run() {
	for {
		select {
		case <-m.stopChan:
			return
		case <-m.wake:
		}
		for {
			select {
			case <-m.stopChan:
				return
			default:
			}
			m.mu.Lock()
			latest := -1
			for i := range m.jobs {
				if m.jobs[i].Status == "queued" {
					latest = i
				}
			}
			if latest < 0 {
				m.mu.Unlock()
				break
			}
			selected := m.jobs[latest]
			for i := range m.jobs {
				if i != latest && m.jobs[i].Status == "queued" {
					m.jobs[i].Status = "superseded"
					m.jobs[i].UpdatedAt = time.Now().UTC()
				}
			}
			m.jobs[latest].Status = "running"
			m.jobs[latest].UpdatedAt = time.Now().UTC()
			_ = m.persistLocked()
			m.mu.Unlock()
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				select {
				case <-m.stopChan:
					return
				default:
				}
				m.set(selected.ID, "running", "", attempt)
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				err = m.publish(ctx, func(stage string) { m.set(selected.ID, stage, "", attempt) })
				cancel()
				if err == nil {
					break
				}
				if attempt < 3 {
					select {
					case <-m.stopChan:
						return
					case <-time.After(time.Duration(attempt) * time.Second):
					}
				}
			}
			if err == nil {
				m.set(selected.ID, "succeeded", "", 0)
			} else {
				m.set(selected.ID, "failed", err.Error(), 0)
			}
		}
	}
}
func (m *Manager) Retry(id string) (Job, error) {
	m.mu.Lock()
	found := false
	for _, j := range m.jobs {
		if j.ID == id && j.Status == "failed" {
			found = true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return Job{}, errors.New("job not found or not failed")
	}
	return m.Enqueue() // recompile newest desired state, never replay stale configuration
}
