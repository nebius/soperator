package sconfigcontroller

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Fs interface {
	MkdirAll(path string, mode os.FileMode) error

	PrepareNewFile(oldFile string, content []byte, mode os.FileMode) (tempFileName string, err error)

	RenameExchange(oldPath, newPath string) error

	RenameNoReplace(oldPath, newPath string) error

	Remove(name string) error

	SyncCaches() error
}

type PrefixFs struct {
	Prefix string
}

var _ Fs = &PrefixFs{}

// A proper way to implement this would be to do `chroot`/`pivot_root` into jail directory and allow arbitrary paths
// That would correctly resolve symlinks, and path traversing would not matter
// Only issue would be relative paths, and only because we have to decide "relative to what".
// Those are simple to check and disallow, or to resolve from jail root
// `filepath.EvalSymlinks` does not work exactly as we need here because when it sees symlink it just
// uses it as prefix, which is not correct when it is absolute. It should join `pfs.Prefix` instead
// Roughly same goes for relative symlinks: symlink like `../../..` should be capped at jail root
// So, instead of implementing proper support for traversal and symlinks this just denies any path touching symlinks
func (pfs *PrefixFs) checkPath(innerPath string) error {
	if !filepath.IsAbs(innerPath) {
		return fmt.Errorf("path is not absolute")
	}

	acc := pfs.Prefix
	segments := strings.Split(innerPath, string(filepath.Separator))
	for _, segment := range segments {
		if segment == "." || segment == ".." {
			return fmt.Errorf("path traversal is not supported")
		}

		acc = filepath.Join(acc, segment)
		if acc == pfs.Prefix {
			// Trust pfs.Prefix
			continue
		}

		fi, err := os.Lstat(acc)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// It's OK if there's no intermediate directory or final file, they will be created
				// But it still should check full path because of possible path traversal
				continue
			}
			return err
		}

		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlinks in path are not supported: %q", acc)
		}
	}

	return nil
}

func (pfs *PrefixFs) addPrefix(innerPath string) (string, error) {
	if err := pfs.checkPath(innerPath); err != nil {
		return "", err
	}
	return filepath.Join(pfs.Prefix, innerPath), nil
}

func (pfs *PrefixFs) removePrefix(path string) (string, error) {
	trimmed, found := strings.CutPrefix(path, pfs.Prefix)
	if !found {
		return "", fmt.Errorf("path %q does not have prefix %q", path, pfs.Prefix)
	}

	return trimmed, nil
}

func (pfs *PrefixFs) MkdirAll(path string, mode os.FileMode) error {
	prefixed, err := pfs.addPrefix(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(prefixed, mode)
}

func (pfs *PrefixFs) PrepareNewFile(oldFile string, content []byte, mode os.FileMode) (tempFileName string, err error) {
	prefixed, err := pfs.addPrefix(oldFile)
	if err != nil {
		return "", err
	}

	baseName := filepath.Base(prefixed)
	pattern := baseName + "-sconfigcontroller-*"
	dirPath := filepath.Dir(prefixed)

	tempFile, err := os.CreateTemp(dirPath, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tempFileName = tempFile.Name()
	deferTempFileRemove := true

	defer func() {
		if deferTempFileRemove {
			// Remove left-over temp file
			err = errors.Join(err, os.Remove(tempFileName))
		}
	}()

	defer func() {
		if tempFile != nil {
			// Errors from file.Close() are especially important on NFS/virtiofs/... for close-to-open sync
			// man 5 nfs
			// > Close-to-open cache consistency
			// > ...
			// > When the application closes the file, the NFS client writes back
			// > any pending changes to the file so that the next opener can view
			// > the changes.  This also gives the NFS client an opportunity to
			// > report write errors to the application via the return code from
			// > close(2).
			err = errors.Join(err, tempFile.Close())
		}
	}()

	if _, err = tempFile.Write(content); err != nil {
		err = fmt.Errorf("write temp file: %w", err)
		return "", err
	}

	err = tempFile.Close()
	// Resetting here to avoid double call to Close in defer
	tempFile = nil
	if err != nil {
		err = fmt.Errorf("close temp file: %w", err)
		return "", err
	}

	err = os.Chmod(tempFileName, mode)
	if err != nil {
		err = fmt.Errorf("chmod temp file: %w", err)
		return "", err
	}

	// Caller take control over temp file
	deferTempFileRemove = false
	// tempFileName is a path returned directly from FS, and it contains current prefix,
	// which is not okay to feed back to prefix fs methods
	return pfs.removePrefix(tempFileName)
}

func (pfs *PrefixFs) RenameExchange(oldPath, newPath string) error {
	prefixedOld, err := pfs.addPrefix(oldPath)
	if err != nil {
		return err
	}
	prefixedNew, err := pfs.addPrefix(newPath)
	if err != nil {
		return err
	}

	return renameExchange(prefixedOld, prefixedNew)
}

func (pfs *PrefixFs) RenameNoReplace(oldPath, newPath string) error {
	prefixedOld, err := pfs.addPrefix(oldPath)
	if err != nil {
		return err
	}
	prefixedNew, err := pfs.addPrefix(newPath)
	if err != nil {
		return err
	}

	return renameNoReplace(prefixedOld, prefixedNew)
}

func (pfs *PrefixFs) Remove(name string) error {
	prefixed, err := pfs.addPrefix(name)
	if err != nil {
		return err
	}

	return os.Remove(prefixed)
}

func (pfs *PrefixFs) SyncCaches() error {
	// Some filesystems can keep directory entries caches for too long without invalidating
	// This delay is expected to make these caches stale on worker VMs
	// So when slurmd will restart, it will pick up new inodes for config files
	// Also it should keep inodes for old files alive while caches are alive, because only directory entries are cached, not inodes themselves
	// Deleting earlier can lead to "file not found" errors
	time.Sleep(delayBetweenWriteAndReconfigure)
	return nil
}
