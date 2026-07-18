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

	"golang.org/x/sys/unix"
)

const (
	DefaultHashLimit  int64 = 16 << 20
	DefaultTotalLimit int64 = 32 << 20
	maxSnapshotDepth        = 64
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

type Reader struct{ roots []allowedRoot }

func NewReader(roots ...string) (*Reader, error) {
	if len(roots) == 0 {
		return nil, errors.New("at least one allowlisted root is required")
	}
	seen := map[string]bool{}
	cleaned := make([]allowedRoot, 0, len(roots))
	closeRoots := func() {
		for _, root := range cleaned {
			_ = unix.Close(root.fd)
		}
	}
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || !filepath.IsAbs(root) {
			closeRoots()
			return nil, fmt.Errorf("allowlisted root must be absolute: %q", root)
		}
		if root == string(filepath.Separator) {
			closeRoots()
			return nil, errors.New("filesystem root cannot be allowlisted")
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			closeRoots()
			return nil, fmt.Errorf("resolve allowlisted root %s: %w", root, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			closeRoots()
			return nil, fmt.Errorf("stat allowlisted root %s: %w", root, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			closeRoots()
			return nil, fmt.Errorf("allowlisted root must be a directory or regular file: %s", root)
		}
		if !seen[root] {
			fd, err := unix.Open(canonical, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				closeRoots()
				return nil, fmt.Errorf("open allowlisted root %s: %w", root, err)
			}
			seen[root] = true
			cleaned = append(cleaned, allowedRoot{display: root, canonical: canonical, fd: fd})
		}
	}
	sort.Slice(cleaned, func(i, j int) bool { return cleaned[i].display < cleaned[j].display })
	return &Reader{roots: cleaned}, nil
}

func (r *Reader) Roots() []string {
	values := make([]string, 0, len(r.roots))
	for _, root := range r.roots {
		values = append(values, root.display)
	}
	return values
}

func (r *Reader) Resolve(path string) (string, error) {
	file, err := r.openPath(path, unix.O_PATH)
	if err != nil {
		return "", err
	}
	defer file.Close()
	resolved, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", file.Fd()))
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func (r *Reader) Metadata(ctx context.Context, path string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	file, err := r.openPath(path, unix.O_PATH)
	if err != nil {
		return Metadata{}, err
	}
	defer file.Close()
	info, err := file.Stat()
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
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	pathFile, err := r.openPath(path, unix.O_PATH)
	if err != nil {
		return nil, false, err
	}
	defer pathFile.Close()
	info, err := pathFile.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, errors.New("path is not a directory")
	}
	directory, err := reopenPathFile(pathFile, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, false, err
	}
	defer directory.Close()
	var out []Metadata
	truncated := false
	err = walkDirectory(ctx, directory, "", 0, func(relative string, _ int, _ *os.File, info os.FileInfo, symlink bool) (bool, bool, error) {
		if len(out) >= limit {
			truncated = true
			return false, true, nil
		}
		if symlink {
			out = append(out, Metadata{Path: filepath.Join(path, relative), Name: filepath.Base(relative), Mode: os.ModeSymlink.String()})
			return false, false, nil
		}
		out = append(out, metadata(filepath.Join(path, relative), info))
		return false, false, nil
	})
	if err != nil {
		return nil, false, err
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
	if err := ctx.Err(); err != nil {
		return FileHash{}, err
	}
	file, err := r.openPath(path, unix.O_RDONLY)
	if err != nil {
		return FileHash{}, err
	}
	defer file.Close()
	hash, info, bytesRead, err := hashOpenFile(ctx, file, maxBytes)
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
	var found []Metadata
	err := r.walk(ctx, path, func(relative string, depth int, _ *os.File, info os.FileInfo, symlink bool) (bool, bool, error) {
		if symlink || depth > maxDepth {
			return false, false, nil
		}
		if info.IsDir() {
			return depth < maxDepth, false, nil
		}
		if info.Mode().IsRegular() && info.Size() >= minimumBytes {
			found = append(found, metadata(filepath.Join(path, relative), info))
		}
		return false, false, nil
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
	out := DirectoryUsage{Path: path}
	visited := 0
	err := r.walk(ctx, path, func(_ string, depth int, _ *os.File, info os.FileInfo, symlink bool) (bool, bool, error) {
		if depth == 0 {
			if !info.IsDir() {
				return false, false, errors.New("path is not a directory")
			}
			return true, false, nil
		}
		if symlink {
			out.Skipped++
			return false, false, nil
		}
		if info.IsDir() && depth > maxDepth {
			out.Truncated = true
			return false, false, nil
		}
		if depth > maxDepth {
			out.Truncated = true
			return false, false, nil
		}
		if visited >= maxEntries {
			out.Truncated = true
			return false, true, nil
		}
		visited++
		if info.IsDir() {
			out.Directories++
			return depth < maxDepth, false, nil
		}
		if info.Mode().IsRegular() {
			out.Files++
			out.SizeBytes += info.Size()
		} else {
			out.Skipped++
		}
		return false, false, nil
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
	out := Snapshot{Root: path}
	err := r.walk(ctx, path, func(relative string, depth int, file *os.File, info os.FileInfo, symlink bool) (bool, bool, error) {
		if len(out.Entries)+len(out.Skipped) >= limit {
			out.Truncated = true
			return false, true, nil
		}
		if symlink {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: relative, Reason: "symlink"})
			return false, false, nil
		}
		if info.IsDir() {
			if depth >= maxSnapshotDepth {
				out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: relative, Reason: "depth_limit"})
				out.Truncated = true
				return false, false, nil
			}
			return true, false, nil
		}
		if !info.Mode().IsRegular() {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: relative, Reason: "not_regular"})
			return false, false, nil
		}
		if info.Size() > maxFileBytes {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: relative, Reason: "file_limit"})
			return false, false, nil
		}
		if out.HashedBytes+info.Size() > maxTotalBytes {
			out.Skipped = append(out.Skipped, SkippedEntry{RelativePath: relative, Reason: "total_limit"})
			return false, false, nil
		}
		readable, err := reopenPathFile(file, unix.O_RDONLY)
		if err != nil {
			return false, false, err
		}
		hash, currentInfo, bytesRead, err := hashOpenFile(ctx, readable, maxFileBytes)
		closeErr := readable.Close()
		if err != nil {
			return false, false, errors.Join(err, closeErr)
		}
		if closeErr != nil {
			return false, false, closeErr
		}
		out.HashedBytes += bytesRead
		out.Entries = append(out.Entries, SnapshotEntry{RelativePath: relative, SizeBytes: currentInfo.Size(), Modified: currentInfo.ModTime().UTC(), SHA256: hash})
		return false, false, nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].RelativePath < out.Entries[j].RelativePath })
	sort.Slice(out.Skipped, func(i, j int) bool { return out.Skipped[i].RelativePath < out.Skipped[j].RelativePath })
	return out, nil
}

func metadata(path string, info os.FileInfo) Metadata {
	return Metadata{Path: filepath.Clean(path), Name: info.Name(), SizeBytes: info.Size(), Mode: info.Mode().String(), IsDir: info.IsDir(), IsRegular: info.Mode().IsRegular(), Modified: info.ModTime().UTC()}
}

func hashOpenFile(ctx context.Context, file *os.File, maxBytes int64) (string, os.FileInfo, int64, error) {
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
