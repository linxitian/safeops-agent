package safefs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSecureOpenAtFallsBackOnENOSYSWithoutFollowingSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "value"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "final-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "directory-link")); err != nil {
		t.Fatal(err)
	}

	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	unavailable := func(int, string, *unix.OpenHow) (int, error) { return -1, unix.ENOSYS }
	fd, err := secureOpenAtWith(rootFD, "nested/value", unix.O_RDONLY, unavailable)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "nested/value")
	value, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(value) != "safe" {
		t.Fatalf("fallback read = %q, read error %v, close error %v", value, readErr, closeErr)
	}

	for _, path := range []string{"final-link", "directory-link/secret", "../secret", "/absolute", "nested/../nested/value", "nested//value"} {
		if fd, err := secureOpenAtWith(rootFD, path, unix.O_PATH, unavailable); err == nil {
			_ = unix.Close(fd)
			t.Fatalf("unsafe fallback path was accepted: %q", path)
		}
	}
}

func TestSecureOpenAtDoesNotFallbackOnPolicyError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	denied := func(int, string, *unix.OpenHow) (int, error) { return -1, unix.EPERM }
	fd, err := secureOpenAtWith(rootFD, "value", unix.O_RDONLY, denied)
	if fd >= 0 {
		_ = unix.Close(fd)
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatalf("policy error was replaced or bypassed: %v", err)
	}
}
