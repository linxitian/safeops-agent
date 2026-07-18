package safefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultHashLimit  int64 = 16 << 20
	DefaultTotalLimit int64 = 32 << 20
)

type Metadata struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	Mode      string    `json:"mode"`
	IsDir     bool      `json:"is_dir"`
	IsRegular bool      `json:"is_regular"`
	Modified  time.Time `json:"modified"`
}

type FileHash struct {
	Metadata  Metadata `json:"metadata"`
	SHA256    string   `json:"sha256"`
	BytesRead int64    `json:"bytes_read"`
}

type SnapshotEntry struct {
	RelativePath string    `json:"relative_path"`
	SizeBytes    int64     `json:"size_bytes"`
	Modified     time.Time `json:"modified"`
	SHA256       string    `json:"sha256"`
}

type SkippedEntry struct {
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

type Snapshot struct {
	Root        string          `json:"root"`
	Entries     []SnapshotEntry `json:"entries"`
	Skipped     []SkippedEntry  `json:"skipped"`
	HashedBytes int64           `json:"hashed_bytes"`
	Truncated   bool            `json:"truncated"`
}

type DirectoryUsage struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	Files       int    `json:"files"`
	Directories int    `json:"directories"`
	Skipped     int    `json:"skipped"`
	Truncated   bool   `json:"truncated"`
}

type Reader struct{ roots []string }

func NewReader(roots ...string) (*Reader, error) {
	if len(roots) == 0 {
		return nil, errors.New("at least one allowlisted root is required")
	}
	seen := map[string]bool{}
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("allowlisted root must be absolute: %q", root)
		}
		if root == string(filepath.Separator) {
			return nil, errors.New("filesystem root cannot be allowlisted")
		}
		if !seen[root] {
			seen[root] = true
			cleaned = append(cleaned, root)
		}
	}
	sort.Strings(cleaned)
	return &Reader{roots: cleaned}, nil
}

func (r *Reader) Roots() []string { return append([]string(nil), r.roots...) }

func (r *Reader) Resolve(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	for _, root := range r.roots {
		if !within(root, path) {
			continue
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", fmt.Errorf("resolve allowlisted root %s: %w", root, err)
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve path %s: %w", path, err)
		}
		if !within(resolvedRoot, resolvedPath) {
			return "", errors.New("path escapes allowlisted root through symlink")
		}
		return resolvedPath, nil
	}
	return "", errors.New("path is outside all allowlisted roots")
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (r *Reader) Metadata(ctx context.Context, path string) (Metadata, error) {
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return Metadata{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Metadata{}, err
	}
	return metadata(path, info), nil
}

func (r *Reader) List(ctx context.Context, path string, limit int) ([]Metadata, bool, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		return nil, false, errors.New("limit must not exceed 500")
	}
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return nil, false, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, false, err
	}
	truncated := len(entries) > limit
	if len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, false, err
		}
		out = append(out, metadata(filepath.Join(path, entry.Name()), info))
	}
	return out, truncated, nil
}

func (r *Reader) Hash(ctx context.Context, path string, maxBytes int64) (FileHash, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultHashLimit
	}
	if maxBytes > DefaultHashLimit {
		return FileHash{}, fmt.Errorf("max_bytes must not exceed %d", DefaultHashLimit)
	}
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return FileHash{}, err
	}
	hash, info, bytesRead, err := hashFile(ctx, resolved, maxBytes)
	if err != nil {
		return FileHash{}, err
	}
	return FileHash{Metadata: metadata(path, info), SHA256: hash, BytesRead: bytesRead}, nil
}

func (r *Reader) FindLarge(ctx context.Context, path string, minimumBytes int64, maxDepth, limit int) ([]Metadata, bool, error) {
	if minimumBytes < 0 {
		return nil, false, errors.New("minimum_bytes must not be negative")
	}
	if maxDepth <= 0 {
		maxDepth = 4
	}
	if maxDepth > 16 {
		return nil, false, errors.New("max_depth must not exceed 16")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		return nil, false, errors.New("limit must not exceed 200")
	}
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return nil, false, err
	}
	var found []Metadata
	err = filepath.WalkDir(resolved, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, current)
		if err != nil {
			return err
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(filepath.Separator)))
		}
		if entry.IsDir() && depth > maxDepth {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || depth > maxDepth {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Size() >= minimumBytes {
			found = append(found, metadata(filepath.Join(path, rel), info))
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].SizeBytes == found[j].SizeBytes {
			return found[i].Path < found[j].Path
		}
		return found[i].SizeBytes > found[j].SizeBytes
	})
	truncated := len(found) > limit
	if truncated {
		found = found[:limit]
	}
	return found, truncated, nil
}

// Usage computes metadata-only directory usage under an allowlisted root. It
// never opens file contents, follows no symlinks, and has explicit depth and
// entry bounds.
func (r *Reader) Usage(ctx context.Context, path string, maxDepth, maxEntries int) (DirectoryUsage, error) {
	if maxDepth <= 0 {
		maxDepth = 4
	}
	if maxDepth > 16 {
		return DirectoryUsage{}, errors.New("max_depth must not exceed 16")
	}
	if maxEntries <= 0 {
		maxEntries = 5000
	}
	if maxEntries > 20000 {
		return DirectoryUsage{}, errors.New("max_entries must not exceed 20000")
	}
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return DirectoryUsage{}, err
	}
	out := DirectoryUsage{Path: path}
	visited := 0
	err = filepath.WalkDir(resolved, func(current string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			out.Skipped++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if current == resolved {
			return nil
		}
		rel, err := filepath.Rel(resolved, current)
		if err != nil {
			return err
		}
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if entry.Type()&os.ModeSymlink != 0 {
			out.Skipped++
			return nil
		}
		if entry.IsDir() && depth > maxDepth {
			out.Truncated = true
			return filepath.SkipDir
		}
		if depth > maxDepth {
			out.Truncated = true
			return nil
		}
		if visited >= maxEntries {
			out.Truncated = true
			return filepath.SkipAll
		}
		visited++
		if entry.IsDir() {
			out.Directories++
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			out.Skipped++
			return nil
		}
		if info.Mode().IsRegular() {
			out.Files++
			out.SizeBytes += info.Size()
		} else {
			out.Skipped++
		}
		return nil
	})
	return out, err
}

func (r *Reader) Snapshot(ctx context.Context, path string, limit int, maxFileBytes, maxTotalBytes int64) (Snapshot, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		return Snapshot{}, errors.New("limit must not exceed 500")
	}
	if maxFileBytes <= 0 {
		maxFileBytes = 4 << 20
	}
	if maxFileBytes > DefaultHashLimit {
		return Snapshot{}, fmt.Errorf("max_file_bytes must not exceed %d", DefaultHashLimit)
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = DefaultTotalLimit
	}
	if maxTotalBytes > DefaultTotalLimit {
		return Snapshot{}, fmt.Errorf("max_total_bytes must not exceed %d", DefaultTotalLimit)
	}
	resolved, err := r.resolveContext(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Root: path}
	err = filepath.WalkDir(resolved, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(resolved, current)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: rel, Reason: "symlink"})
			return nil
		}
		if len(out.Entries)+len(out.Skipped) >= limit {
			out.Truncated = true
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: rel, Reason: "not_regular"})
			return nil
		}
		if info.Size() > maxFileBytes {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: rel, Reason: "file_limit"})
			return nil
		}
		if out.HashedBytes+info.Size() > maxTotalBytes {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: rel, Reason: "total_limit"})
			return nil
		}
		hash, currentInfo, bytesRead, err := hashFile(ctx, current, maxFileBytes)
		if err != nil {
			return err
		}
		out.HashedBytes += bytesRead
		out.Entries = append(out.Entries, SnapshotEntry{RelativePath: rel, SizeBytes: currentInfo.Size(), Modified: currentInfo.ModTime().UTC(), SHA256: hash})
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].RelativePath < out.Entries[j].RelativePath })
	sort.Slice(out.Skipped, func(i, j int) bool { return out.Skipped[i].RelativePath < out.Skipped[j].RelativePath })
	return out, nil
}

func (r *Reader) resolveContext(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.Resolve(path)
}

func metadata(path string, info os.FileInfo) Metadata {
	return Metadata{Path: filepath.Clean(path), Name: info.Name(), SizeBytes: info.Size(), Mode: info.Mode().String(), IsDir: info.IsDir(), IsRegular: info.Mode().IsRegular(), Modified: info.ModTime().UTC()}
}

func hashFile(ctx context.Context, path string, maxBytes int64) (string, os.FileInfo, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, 0, errors.New("path is not a regular file")
	}
	if info.Size() > maxBytes {
		return "", nil, 0, fmt.Errorf("file exceeds max_bytes %d", maxBytes)
	}
	h := sha256.New()
	written, err := io.Copy(h, &contextReader{ctx: ctx, reader: io.LimitReader(file, maxBytes+1)})
	if err != nil {
		return "", nil, written, err
	}
	if written > maxBytes {
		return "", nil, written, fmt.Errorf("file grew beyond max_bytes %d", maxBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), info, written, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
