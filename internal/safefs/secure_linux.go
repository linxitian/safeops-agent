package safefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type allowedRoot struct {
	display   string
	canonical string
	fd        int
}

const secureResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS

type openat2Func func(int, string, *unix.OpenHow) (int, error)

func secureOpenAt(directoryFD int, relative string, flags uint64) (int, error) {
	return secureOpenAtWith(directoryFD, relative, flags, unix.Openat2)
}

func secureOpenAtWith(directoryFD int, relative string, flags uint64, openat2 openat2Func) (int, error) {
	if _, err := securePathComponents(relative); err != nil {
		return -1, err
	}
	fd, err := openat2(directoryFD, relative, &unix.OpenHow{
		Flags:   flags | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: secureResolveFlags,
	})
	if err == nil || !errors.Is(err, unix.ENOSYS) {
		return fd, err
	}
	return openatNoSymlinks(directoryFD, relative, flags)
}

func securePathComponents(relative string) ([]string, error) {
	if relative == "" || relative == "." || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return nil, errors.New("secure path must be a clean relative path")
	}
	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, errors.New("secure path contains a forbidden component")
		}
	}
	return components, nil
}

func openatNoSymlinks(rootFD int, relative string, flags uint64) (int, error) {
	components, err := securePathComponents(relative)
	if err != nil {
		return -1, err
	}
	directoryFD := rootFD
	ownedDirectoryFD := -1
	for index, component := range components {
		last := index == len(components)-1
		openFlags := unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY
		if last {
			openFlags = int(flags) | unix.O_CLOEXEC | unix.O_NOFOLLOW
		}
		fd, openErr := unix.Openat(directoryFD, component, openFlags, 0)
		if ownedDirectoryFD >= 0 {
			_ = unix.Close(ownedDirectoryFD)
			ownedDirectoryFD = -1
		}
		if openErr != nil {
			return -1, openErr
		}
		var stat unix.Stat_t
		if statErr := unix.Fstat(fd, &stat); statErr != nil {
			_ = unix.Close(fd)
			return -1, statErr
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			_ = unix.Close(fd)
			return -1, errors.New("symlink paths are not allowed")
		}
		if last {
			return fd, nil
		}
		directoryFD = fd
		ownedDirectoryFD = fd
	}
	return -1, errors.New("secure path has no components")
}

func (r *Reader) rootFor(path string) (allowedRoot, string, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(clean) {
		return allowedRoot{}, "", errors.New("path must be absolute")
	}
	var selected allowedRoot
	var relative string
	for _, root := range r.roots {
		rel, err := filepath.Rel(root.display, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if selected.display == "" || len(root.display) > len(selected.display) {
			selected = root
			relative = rel
		}
	}
	if selected.display == "" {
		return allowedRoot{}, "", errors.New("path is outside all allowlisted roots")
	}
	return selected, relative, nil
}

func (r *Reader) openPath(path string, flags uint64) (*os.File, error) {
	root, relative, err := r.rootFor(path)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		rootFD, err := unix.Dup(root.fd)
		if err != nil {
			return nil, fmt.Errorf("duplicate allowlisted root %s: %w", root.display, err)
		}
		unix.CloseOnExec(rootFD)
		rootFile := os.NewFile(uintptr(rootFD), root.canonical)
		if flags&unix.O_PATH != 0 {
			return rootFile, nil
		}
		defer rootFile.Close()
		return reopenPathFile(rootFile, int(flags))
	}
	fd, err := secureOpenAt(root.fd, relative, flags)
	if err != nil {
		return nil, fmt.Errorf("securely open %s: %w", filepath.Clean(path), err)
	}
	file := os.NewFile(uintptr(fd), filepath.Clean(path))
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		file.Close()
		return nil, errors.New("symlink paths are not allowed")
	}
	return file, nil
}

func reopenPathFile(pathFile *os.File, flags int) (*os.File, error) {
	info, err := pathFile.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		fd, err := unix.Openat(int(pathFile.Fd()), ".", flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), pathFile.Name()), nil
	}
	file, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", pathFile.Fd()))
	if err != nil {
		return nil, err
	}
	return file, nil
}

type secureVisitor func(relative string, depth int, file *os.File, info os.FileInfo, symlink bool) (descend bool, stop bool, err error)

func (r *Reader) walk(ctx context.Context, path string, visit secureVisitor) error {
	root, err := r.openPath(path, unix.O_PATH)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		_, _, err := visit(".", 0, root, info, false)
		return err
	}
	directory, err := reopenPathFile(root, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer directory.Close()
	return walkDirectory(ctx, directory, "", 0, visit)
}

func walkDirectory(ctx context.Context, directory *os.File, prefix string, parentDepth int, visit secureVisitor) error {
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := filepath.Join(prefix, entry.Name())
		depth := parentDepth + 1
		if entry.Type()&os.ModeSymlink != 0 {
			_, stop, err := visit(relative, depth, nil, nil, true)
			if err != nil || stop {
				return err
			}
			continue
		}
		fd, err := secureOpenAt(int(directory.Fd()), entry.Name(), unix.O_PATH)
		if err != nil {
			if errors.Is(err, unix.ELOOP) {
				_, stop, visitErr := visit(relative, depth, nil, nil, true)
				if visitErr != nil || stop {
					return visitErr
				}
				continue
			}
			return fmt.Errorf("securely open %s: %w", relative, err)
		}
		pathFile := os.NewFile(uintptr(fd), relative)
		info, statErr := pathFile.Stat()
		if statErr != nil {
			pathFile.Close()
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			pathFile.Close()
			_, stop, visitErr := visit(relative, depth, nil, nil, true)
			if visitErr != nil || stop {
				return visitErr
			}
			continue
		}
		descend, stop, visitErr := visit(relative, depth, pathFile, info, false)
		if visitErr != nil || stop {
			pathFile.Close()
			return visitErr
		}
		if descend && info.IsDir() {
			child, openErr := reopenPathFile(pathFile, unix.O_RDONLY|unix.O_DIRECTORY)
			if openErr != nil {
				pathFile.Close()
				return openErr
			}
			walkErr := walkDirectory(ctx, child, relative, depth, visit)
			closeErr := child.Close()
			if walkErr != nil || closeErr != nil {
				pathFile.Close()
				return errors.Join(walkErr, closeErr)
			}
		}
		if err := pathFile.Close(); err != nil {
			return err
		}
	}
	return nil
}
