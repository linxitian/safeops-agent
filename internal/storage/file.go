package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrTaskLeased    = errors.New("task is leased by another worker")
	ErrLeaseConflict = errors.New("task worker lease conflict")
	ErrLeaseExpired  = errors.New("task worker lease expired")
)

type FileStore struct {
	root string
	mu   sync.RWMutex
	now  func() time.Time
}

type taskPreparation struct {
	SchemaVersion int             `json:"schema_version"`
	Task          task.Task       `json:"task"`
	Session       session.Session `json:"session"`
}

func NewFileStore(root string) (*FileStore, error) {
	for _, dir := range []string{"sessions", "tasks", "approvals", "traces", "quarantine", "state", "lab"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			return nil, fmt.Errorf("create data directory %s: %w", dir, err)
		}
	}
	store := &FileStore{root: root, now: time.Now}
	if err := store.withExclusive(context.Background(), store.recoverTaskPreparationsLocked); err != nil {
		return nil, fmt.Errorf("recover task preparations: %w", err)
	}
	return store, nil
}

func (s *FileStore) Root() string { return s.root }

func (s *FileStore) SaveSession(ctx context.Context, value session.Session) error {
	if err := validateID(value.ID); err != nil {
		return err
	}
	return s.withExclusive(ctx, func() error { return s.saveLocked(ctx, "sessions", value.ID, value) })
}
func (s *FileStore) UpdateSession(ctx context.Context, id string, mutate func(*session.Session) error) (session.Session, error) {
	if err := validateID(id); err != nil {
		return session.Session{}, err
	}
	if mutate == nil {
		return session.Session{}, errors.New("session mutation is required")
	}
	var value session.Session
	err := s.withExclusive(ctx, func() error {
		if err := s.loadLocked("sessions", id, &value); err != nil {
			return err
		}
		if err := mutate(&value); err != nil {
			return err
		}
		if value.ID != id {
			return errors.New("session mutation changed the session id")
		}
		return s.saveLocked(ctx, "sessions", id, value)
	})
	return value, err
}
func (s *FileStore) GetSession(ctx context.Context, id string) (session.Session, error) {
	var out session.Session
	err := s.get(ctx, "sessions", id, &out)
	return out, err
}
func (s *FileStore) ListSessions(ctx context.Context) ([]session.Session, error) {
	var out []session.Session
	err := s.list(ctx, "sessions", func() any { return &session.Session{} }, func(v any) { out = append(out, *v.(*session.Session)) })
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, err
}

func (s *FileStore) PrepareTask(ctx context.Context, value task.Task, mutate func(*session.Session) error) (session.Session, error) {
	if err := validateID(value.ID); err != nil {
		return session.Session{}, err
	}
	if err := validateID(value.SessionID); err != nil {
		return session.Session{}, err
	}
	if mutate == nil {
		return session.Session{}, errors.New("session mutation is required")
	}
	var prepared session.Session
	err := s.withExclusive(ctx, func() error {
		var existing task.Task
		if err := s.loadLocked("tasks", value.ID, &existing); err == nil {
			return errors.New("task already exists")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := s.loadLocked("sessions", value.SessionID, &prepared); err != nil {
			return err
		}
		if err := mutate(&prepared); err != nil {
			return err
		}
		if prepared.ID != value.SessionID {
			return errors.New("session mutation changed the session id")
		}
		messageCount := 0
		for _, message := range prepared.Messages {
			if message.TaskID == value.ID {
				messageCount++
			}
		}
		if messageCount != 1 {
			return errors.New("prepared session must contain exactly one message bound to the task")
		}
		record := taskPreparation{SchemaVersion: 1, Task: value, Session: prepared}
		if err := s.saveLocked(ctx, "state", preparationID(value.ID), record); err != nil {
			return err
		}
		if err := s.saveLocked(ctx, "tasks", value.ID, value); err != nil {
			return err
		}
		if err := s.saveLocked(ctx, "sessions", prepared.ID, prepared); err != nil {
			return err
		}
		return s.removePreparationLocked(value.ID)
	})
	return prepared, err
}
func (s *FileStore) SaveTask(ctx context.Context, value task.Task) error {
	if err := validateID(value.ID); err != nil {
		return err
	}
	return s.withExclusive(ctx, func() error {
		var current task.Task
		err := s.loadLocked("tasks", value.ID, &current)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			if leaseErr := s.validateTaskWrite(current, value); leaseErr != nil {
				return leaseErr
			}
		}
		return s.saveLocked(ctx, "tasks", value.ID, value)
	})
}
func (s *FileStore) ClaimTask(ctx context.Context, id, owner, token string, ttl time.Duration) (task.Task, error) {
	if err := validateID(id); err != nil {
		return task.Task{}, err
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(token) == "" {
		return task.Task{}, errors.New("worker owner and token are required")
	}
	if ttl <= 0 || ttl > 10*time.Minute {
		return task.Task{}, errors.New("worker lease TTL must be within (0, 10m]")
	}
	var value task.Task
	err := s.withExclusive(ctx, func() error {
		if err := s.loadLocked("tasks", id, &value); err != nil {
			return err
		}
		now := s.now().UTC()
		if value.WorkerLease.Token != "" && now.Before(value.WorkerLease.ExpiresAt) {
			return ErrTaskLeased
		}
		value.WorkerLease = task.WorkerLease{Owner: strings.TrimSpace(owner), Token: strings.TrimSpace(token), Fence: value.WorkerLease.Fence + 1, ExpiresAt: now.Add(ttl)}
		return s.saveLocked(ctx, "tasks", id, value)
	})
	return value, err
}
func (s *FileStore) ReleaseTask(ctx context.Context, id, token string, fence uint64) (task.Task, error) {
	if err := validateID(id); err != nil {
		return task.Task{}, err
	}
	var value task.Task
	err := s.withExclusive(ctx, func() error {
		if err := s.loadLocked("tasks", id, &value); err != nil {
			return err
		}
		if value.WorkerLease.Token != token || value.WorkerLease.Fence != fence {
			return ErrLeaseConflict
		}
		value.WorkerLease.Owner = ""
		value.WorkerLease.Token = ""
		value.WorkerLease.ExpiresAt = time.Time{}
		return s.saveLocked(ctx, "tasks", id, value)
	})
	return value, err
}
func (s *FileStore) GetTask(ctx context.Context, id string) (task.Task, error) {
	var out task.Task
	err := s.get(ctx, "tasks", id, &out)
	return out, err
}
func (s *FileStore) ListTasks(ctx context.Context) ([]task.Task, error) {
	var out []task.Task
	err := s.list(ctx, "tasks", func() any { return &task.Task{} }, func(v any) { out = append(out, *v.(*task.Task)) })
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, err
}

func validateID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || id == "." || id == ".." {
		return fmt.Errorf("invalid id %q", id)
	}
	return nil
}

func (s *FileStore) saveLocked(ctx context.Context, kind, id string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Join(s.root, kind)
	temp, err := os.CreateTemp(dir, ".safeops-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(b); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func (s *FileStore) loadLocked(kind, id string, out any) error {
	b, err := os.ReadFile(filepath.Join(s.root, kind, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (s *FileStore) validateTaskWrite(current, next task.Task) error {
	lease := current.WorkerLease
	if lease.Token == "" {
		if next.WorkerLease.Token != "" || next.WorkerLease.Fence < lease.Fence {
			return ErrLeaseConflict
		}
		return nil
	}
	if next.WorkerLease.Token != lease.Token || next.WorkerLease.Fence != lease.Fence {
		return ErrLeaseConflict
	}
	if !s.now().UTC().Before(lease.ExpiresAt) {
		return ErrLeaseExpired
	}
	return nil
}

func preparationID(taskID string) string { return "prepare-" + taskID }

func (s *FileStore) recoverTaskPreparationsLocked() error {
	entries, err := os.ReadDir(filepath.Join(s.root, "state"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "prepare-") || filepath.Ext(name) != ".json" {
			continue
		}
		var record taskPreparation
		if err := s.loadLocked("state", strings.TrimSuffix(name, ".json"), &record); err != nil {
			return err
		}
		if record.SchemaVersion != 1 || validateID(record.Task.ID) != nil || validateID(record.Session.ID) != nil || record.Task.SessionID != record.Session.ID {
			return fmt.Errorf("invalid task preparation journal %s", name)
		}
		if err := s.saveLocked(context.Background(), "tasks", record.Task.ID, record.Task); err != nil {
			return err
		}
		if err := s.saveLocked(context.Background(), "sessions", record.Session.ID, record.Session); err != nil {
			return err
		}
		if err := s.removePreparationLocked(record.Task.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileStore) removePreparationLocked(taskID string) error {
	dir := filepath.Join(s.root, "state")
	if err := os.Remove(filepath.Join(dir, preparationID(taskID)+".json")); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func (s *FileStore) withExclusive(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := os.OpenFile(filepath.Join(s.root, "state", "store.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	return operation()
}

func (s *FileStore) get(ctx context.Context, kind, id string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(filepath.Join(s.root, kind, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (s *FileStore) list(ctx context.Context, kind string, alloc func() any, add func(any)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(s.root, kind))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(s.root, kind, entry.Name()))
		if err != nil {
			return err
		}
		v := alloc()
		if err := json.Unmarshal(b, v); err != nil {
			return fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		add(v)
	}
	return nil
}
