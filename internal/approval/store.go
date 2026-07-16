package approval

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

	"safeops-agent/contracts"
	"safeops-agent/internal/id"
)

type Status string

const (
	Pending  Status = "PENDING"
	Approved Status = "APPROVED"
	Rejected Status = "REJECTED"
	Consumed Status = "CONSUMED"
	Expired  Status = "EXPIRED"
)

type Binding struct {
	TaskID               string              `json:"task_id"`
	ProposalDigest       string              `json:"proposal_digest"`
	TargetSnapshotDigest string              `json:"target_snapshot_digest"`
	IntentDigest         string              `json:"intent_digest"`
	PolicyVersion        string              `json:"policy_version"`
	RiskLevel            contracts.RiskLevel `json:"risk_level"`
	Tool                 string              `json:"tool"`
	Nonce                string              `json:"nonce"`
}
type Record struct {
	ID         string    `json:"approval_id"`
	Binding    Binding   `json:"binding"`
	Status     Status    `json:"status"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
	ConsumedAt time.Time `json:"consumed_at,omitempty"`
}
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Store{dir: dir, now: time.Now}, nil
}
func (s *Store) Create(ctx context.Context, binding Binding, ttl time.Duration) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if binding.TaskID == "" || binding.ProposalDigest == "" || binding.TargetSnapshotDigest == "" || binding.IntentDigest == "" || binding.PolicyVersion == "" || binding.Tool == "" {
		return Record{}, errors.New("approval binding is incomplete")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := s.now().UTC()
	record := Record{ID: id.New("approval"), Binding: binding, Status: Pending, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	return record, s.save(record)
}
func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if err := validID(id); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.load(id)
	if err != nil {
		return Record{}, err
	}
	return s.expire(record)
}
func (s *Store) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := s.load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		record, err = s.expire(record)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *Store) Resolve(ctx context.Context, id string, approve bool, reason string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.load(id)
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	if !now.Before(record.ExpiresAt) {
		record.Status = Expired
		record.ResolvedAt = now
		_ = s.save(record)
		return record, errors.New("approval expired")
	}
	desired := Rejected
	if approve {
		desired = Approved
	}
	if record.Status == desired {
		return record, nil
	}
	if record.Status != Pending {
		return record, fmt.Errorf("approval is %s", record.Status)
	}
	record.Status = desired
	record.Reason = strings.TrimSpace(reason)
	record.ResolvedAt = now
	return record, s.save(record)
}
func (s *Store) expire(record Record) (Record, error) {
	if record.Status != Pending || s.now().UTC().Before(record.ExpiresAt) {
		return record, nil
	}
	record.Status = Expired
	record.ResolvedAt = s.now().UTC()
	return record, s.save(record)
}
func (s *Store) Validate(_ context.Context, id string, binding Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.load(id)
	if err != nil {
		return err
	}
	if s.now().After(record.ExpiresAt) {
		return errors.New("approval expired")
	}
	if record.Status != Approved {
		return fmt.Errorf("approval is %s", record.Status)
	}
	if record.Binding != binding {
		return errors.New("approval binding mismatch")
	}
	return nil
}
func (s *Store) Consume(_ context.Context, id string, binding Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.load(id)
	if err != nil {
		return err
	}
	if s.now().After(record.ExpiresAt) {
		return errors.New("approval expired")
	}
	if record.Status != Approved {
		return fmt.Errorf("approval is %s", record.Status)
	}
	if record.Binding != binding {
		return errors.New("approval binding mismatch")
	}
	record.Status = Consumed
	record.ConsumedAt = s.now().UTC()
	return s.save(record)
}
func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }
func validID(value string) error {
	if value == "" || strings.ContainsAny(value, `/\\`) || value == "." || value == ".." {
		return errors.New("invalid approval id")
	}
	return nil
}
func (s *Store) load(id string) (Record, error) {
	if err := validID(id); err != nil {
		return Record{}, err
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return Record{}, err
	}
	var out Record
	if err := json.Unmarshal(b, &out); err != nil {
		return Record{}, err
	}
	return out, nil
}
func (s *Store) save(record Record) error {
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(s.dir, ".approval-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	// Approval records cross the non-root server / privileged executor boundary.
	// The deployment directory is private to the safeops group, so group-read
	// preserves that boundary while allowing a root executor's consumed record
	// to remain readable by the non-root API after the atomic rename.
	if err := f.Chmod(0o640); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path(record.ID))
}
