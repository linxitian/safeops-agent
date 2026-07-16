package rollback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"safeops-agent/contracts"
)

type QuarantineStatus string

const (
	Prepared  QuarantineStatus = "PREPARED"
	Committed QuarantineStatus = "COMMITTED"
	Restored  QuarantineStatus = "RESTORED"
)

var quarantineIDPattern = regexp.MustCompile(`^q_[a-f0-9]{24}$`)

type Manifest struct {
	ID               string                   `json:"quarantine_id"`
	TaskID           string                   `json:"task_id"`
	OriginalPath     string                   `json:"original_path"`
	QuarantinedPath  string                   `json:"quarantined_path"`
	OriginalSnapshot contracts.TargetSnapshot `json:"original_snapshot"`
	Status           QuarantineStatus         `json:"status"`
	CreatedAt        time.Time                `json:"created_at"`
	CommittedAt      time.Time                `json:"committed_at,omitempty"`
	RestoredAt       time.Time                `json:"restored_at,omitempty"`
}

type Operation struct {
	Manifest Manifest `json:"manifest"`
	Checks   []string `json:"checks"`
}

type QuarantineManager struct {
	labRoots       []string
	quarantineRoot string
	objectsDir     string
	manifestsDir   string
	now            func() time.Time
	mu             sync.Mutex
}

func NewQuarantineManager(labRoots []string, quarantineRoot string) (*QuarantineManager, error) {
	if len(labRoots) == 0 {
		return nil, errors.New("at least one Lab root is required")
	}
	canonicalRoots := make([]string, 0, len(labRoots))
	for _, root := range labRoots {
		canonical, err := canonicalDirectory(root)
		if err != nil {
			return nil, fmt.Errorf("Lab root %s: %w", root, err)
		}
		if canonical == string(filepath.Separator) {
			return nil, errors.New("filesystem root cannot be a Lab root")
		}
		canonicalRoots = append(canonicalRoots, canonical)
	}
	if !filepath.IsAbs(quarantineRoot) || filepath.Clean(quarantineRoot) == string(filepath.Separator) {
		return nil, errors.New("quarantine root must be a scoped absolute path")
	}
	for _, directory := range []string{quarantineRoot, filepath.Join(quarantineRoot, "objects"), filepath.Join(quarantineRoot, "manifests")} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, err
		}
	}
	canonicalQuarantine, err := canonicalDirectory(quarantineRoot)
	if err != nil {
		return nil, err
	}
	for _, root := range canonicalRoots {
		if within(root, canonicalQuarantine) || within(canonicalQuarantine, root) {
			return nil, errors.New("quarantine root and Lab roots must not overlap")
		}
	}
	sort.Strings(canonicalRoots)
	return &QuarantineManager{labRoots: canonicalRoots, quarantineRoot: canonicalQuarantine, objectsDir: filepath.Join(canonicalQuarantine, "objects"), manifestsDir: filepath.Join(canonicalQuarantine, "manifests"), now: time.Now}, nil
}

func (m *QuarantineManager) Quarantine(ctx context.Context, taskID, nonce string, expected contracts.TargetSnapshot) (Operation, error) {
	if err := ctx.Err(); err != nil {
		return Operation{}, err
	}
	if expected.Type != "file" || expected.CanonicalPath == "" || taskID == "" || nonce == "" {
		return Operation{}, errors.New("complete file snapshot, task ID, and nonce are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	currentPath, err := filepath.EvalSymlinks(expected.CanonicalPath)
	if err != nil {
		return Operation{}, err
	}
	if !withinAny(currentPath, m.labRoots) {
		return Operation{}, errors.New("file is outside configured Lab roots")
	}
	if err := verifyIdentity(currentPath, expected); err != nil {
		return Operation{}, fmt.Errorf("TARGET_CHANGED: %w", err)
	}
	digest, err := expected.Digest()
	if err != nil {
		return Operation{}, err
	}
	sum := sha256.Sum256([]byte(taskID + "\x00" + nonce + "\x00" + digest))
	id := "q_" + hex.EncodeToString(sum[:12])
	destination := filepath.Join(m.objectsDir, id)
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return Operation{}, errors.New("quarantine object already exists")
		}
		return Operation{}, err
	}
	now := m.now().UTC()
	manifest := Manifest{ID: id, TaskID: taskID, OriginalPath: currentPath, QuarantinedPath: destination, OriginalSnapshot: expected, Status: Prepared, CreatedAt: now}
	if err := m.saveNew(manifest); err != nil {
		return Operation{}, err
	}
	if err := os.Rename(currentPath, destination); err != nil {
		return Operation{}, fmt.Errorf("atomic quarantine rename: %w", err)
	}
	if err := verifyIdentity(destination, expected); err != nil {
		rollbackErr := os.Rename(destination, currentPath)
		return Operation{}, errors.Join(fmt.Errorf("quarantine verification: %w", err), rollbackErr)
	}
	manifest.Status = Committed
	manifest.CommittedAt = m.now().UTC()
	if err := m.save(manifest); err != nil {
		rollbackErr := os.Rename(destination, currentPath)
		return Operation{}, errors.Join(fmt.Errorf("commit quarantine manifest: %w", err), rollbackErr)
	}
	return Operation{Manifest: manifest, Checks: []string{"source snapshot matched", "atomic rename completed", "destination inode/size/mtime/mode matched"}}, nil
}

func (m *QuarantineManager) Restore(ctx context.Context, id string, expected contracts.TargetSnapshot) (Operation, error) {
	if err := ctx.Err(); err != nil {
		return Operation{}, err
	}
	if !quarantineIDPattern.MatchString(id) {
		return Operation{}, errors.New("invalid quarantine ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, err := m.load(id)
	if err != nil {
		return Operation{}, err
	}
	if manifest.Status == Restored {
		return Operation{Manifest: manifest, Checks: []string{"restore already committed"}}, nil
	}
	if manifest.Status != Committed {
		return Operation{}, fmt.Errorf("quarantine is %s", manifest.Status)
	}
	if expected.Type != "file" || filepath.Clean(expected.CanonicalPath) != manifest.QuarantinedPath {
		return Operation{}, errors.New("restore target does not match quarantine manifest")
	}
	if err := verifyIdentity(manifest.QuarantinedPath, expected); err != nil {
		return Operation{}, fmt.Errorf("TARGET_CHANGED: %w", err)
	}
	if err := verifySameIdentity(expected, manifest.OriginalSnapshot); err != nil {
		return Operation{}, fmt.Errorf("quarantined object differs from original manifest: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(manifest.OriginalPath))
	if err != nil || !withinAny(parent, m.labRoots) {
		return Operation{}, errors.New("original parent is unavailable or outside Lab roots")
	}
	if _, err := os.Lstat(manifest.OriginalPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return Operation{}, errors.New("original path is already occupied")
		}
		return Operation{}, err
	}
	if err := os.Rename(manifest.QuarantinedPath, manifest.OriginalPath); err != nil {
		return Operation{}, fmt.Errorf("atomic restore rename: %w", err)
	}
	if err := verifyIdentity(manifest.OriginalPath, manifest.OriginalSnapshot); err != nil {
		rollbackErr := os.Rename(manifest.OriginalPath, manifest.QuarantinedPath)
		return Operation{}, errors.Join(fmt.Errorf("restore verification: %w", err), rollbackErr)
	}
	manifest.Status = Restored
	manifest.RestoredAt = m.now().UTC()
	if err := m.save(manifest); err != nil {
		rollbackErr := os.Rename(manifest.OriginalPath, manifest.QuarantinedPath)
		return Operation{}, errors.Join(fmt.Errorf("commit restore manifest: %w", err), rollbackErr)
	}
	return Operation{Manifest: manifest, Checks: []string{"quarantine snapshot matched", "original path was vacant", "atomic restore completed", "restored inode/size/mtime/mode matched"}}, nil
}

func (m *QuarantineManager) Recover(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.manifestsDir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		manifest, err := m.load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || manifest.Status != Prepared {
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}
		_, sourceErr := os.Lstat(manifest.OriginalPath)
		_, destinationErr := os.Lstat(manifest.QuarantinedPath)
		switch {
		case sourceErr == nil && errors.Is(destinationErr, os.ErrNotExist):
			if err := os.Remove(m.manifestPath(manifest.ID)); err != nil {
				errs = append(errs, err)
			}
		case errors.Is(sourceErr, os.ErrNotExist) && destinationErr == nil:
			if err := verifyIdentity(manifest.QuarantinedPath, manifest.OriginalSnapshot); err != nil {
				errs = append(errs, err)
				continue
			}
			manifest.Status = Committed
			manifest.CommittedAt = m.now().UTC()
			if err := m.save(manifest); err != nil {
				errs = append(errs, err)
			}
		default:
			errs = append(errs, fmt.Errorf("cannot reconcile prepared quarantine %s", manifest.ID))
		}
	}
	return errors.Join(errs...)
}

func (m *QuarantineManager) Get(id string) (Manifest, error) {
	if !quarantineIDPattern.MatchString(id) {
		return Manifest{}, errors.New("invalid quarantine ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load(id)
}

func canonicalDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", errors.New("path must be an existing directory")
	}
	return canonical, nil
}

func verifyIdentity(path string, expected contracts.TargetSnapshot) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("quarantine supports regular files only")
	}
	current := contracts.TargetSnapshot{Type: "file", ID: expected.ID, CanonicalPath: path, Size: info.Size(), MTimeUnixNano: info.ModTime().UnixNano(), Mode: uint32(info.Mode())}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("file inode is unavailable")
	}
	current.Inode = stat.Ino
	return verifySameIdentity(current, expected)
}

func verifySameIdentity(current, expected contracts.TargetSnapshot) error {
	if current.Inode != expected.Inode || current.Size != expected.Size || current.MTimeUnixNano != expected.MTimeUnixNano || current.Mode != expected.Mode {
		return errors.New("inode, size, mtime, or mode changed")
	}
	return nil
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if within(root, path) {
			return true
		}
	}
	return false
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *QuarantineManager) manifestPath(id string) string {
	return filepath.Join(m.manifestsDir, id+".json")
}
func (m *QuarantineManager) load(id string) (Manifest, error) {
	if !quarantineIDPattern.MatchString(id) {
		return Manifest{}, errors.New("invalid quarantine ID")
	}
	b, err := os.ReadFile(m.manifestPath(id))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
func (m *QuarantineManager) saveNew(manifest Manifest) error {
	file, err := os.OpenFile(m.manifestPath(manifest.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		_, err = file.Write(append(b, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	joined := errors.Join(err, closeErr)
	if joined != nil {
		_ = os.Remove(m.manifestPath(manifest.ID))
	}
	return joined
}
func (m *QuarantineManager) save(manifest Manifest) error {
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(m.manifestsDir, ".quarantine-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(b, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.manifestPath(manifest.ID))
}
