package sconfigcontroller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const direntCacheTTL = 15 * time.Second
const delayBetweenWriteAndReconfigure = direntCacheTTL + 1*time.Second

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{
		path: path,
	}
}

func ensureDir(dirPath string) error {
	_, err := os.Stat(dirPath)
	switch {
	case err == nil:
		return nil
	case os.IsNotExist(err):
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("create directory %q: %w", dirPath, err)
		}
		return nil
	default:
		return fmt.Errorf("check directory %q: %w", dirPath, err)
	}
}

func (s *FileStore) Add(name, content, subPath string) (err error) {
	dirPath := filepath.Join(s.path, subPath)
	filePath := filepath.Join(dirPath, name)

	if err = ensureDir(dirPath); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dirPath, name)
	if err != nil {
		err = fmt.Errorf("create temp file: %w", err)
		return err
	}
	tempFileName := tempFile.Name()
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

	if _, err = tempFile.Write([]byte(content)); err != nil {
		err = fmt.Errorf("write temp file: %w", err)
		return err
	}

	err = tempFile.Close()
	// Resetting here to avoid double call to Close in defer
	tempFile = nil
	if err != nil {
		err = fmt.Errorf("close temp file: %w", err)
		return err
	}

	// os.CreateTemp uses 600 & umask by default, and os.Create uses o666 & umask
	// For now fixed 644 should be fine
	// TODO make this configurable
	err = os.Chmod(tempFileName, 0o644)
	if err != nil {
		err = fmt.Errorf("chmod temp file: %w", err)
		return err
	}

	// Don't just rename in case of dirent caches
	// In case this has to work on system without `renameat2`, it can be implemented with os.Link:
	// generate random name, call os.Link, loop if error is "already exists"
	// See os.CreateTemp implementation
	err = unix.Renameat2(unix.AT_FDCWD, tempFileName, unix.AT_FDCWD, filePath, unix.RENAME_EXCHANGE)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have just created tempFileName, so it's most probably about filePath, we can just rename it
			// Using RENAME_NOREPLACE to avoid replacing file created after RENAME_EXCHANGE but before this
			// But at the same time there's no cached dirent, so it should be safe to just rename it and move on
			err = unix.Renameat2(unix.AT_FDCWD, tempFileName, unix.AT_FDCWD, filePath, unix.RENAME_NOREPLACE)
			if err != nil {
				// If rename failed we can just delete temp file and move on
				err = fmt.Errorf("rename temp to target file (%s => %s): %w", tempFileName, filePath, err)
			}
			deferTempFileRemove = false
			return err
		}

		// If exchange failed we can just delete temp file and move on
		err = fmt.Errorf("switch temp and target files (%s and %s): %w", tempFileName, filePath, err)
		return err
	}

	// Some filesystems can keep directory entries caches for too long without invalidating
	// This delay is expected to make these caches stale on worker VMs
	// So when slurmd will restart, it will pick up new inodes for config files
	// Also it should not keep inodes for old files alive, because only directory entries are cached, not inodes themselves
	// Deleting earlier can lead to "file not found" errors

	// Renameat2 has succeeded, and from this moment caches are stale, and we can't just remove temp file
	// without triggering errors on readers
	deferTempFileRemove = false
	// TODO sleep do this once per reconciliation, will be done in #1200
	time.Sleep(delayBetweenWriteAndReconfigure)

	// Delete old entry and free old inode, now that cache is invalidated it should be safe
	err = os.Remove(tempFileName)

	return err
}

func (s *FileStore) SetExecutable(name, subPath string) error {
	filePath := filepath.Join(s.path, subPath, name)
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat file %q: %w", filePath, err)
	}

	// Preserve current perms, add execute bits for u/g/o (0000111 in octal)
	newPerm := info.Mode().Perm() | 0o111

	if err := os.Chmod(filePath, newPerm); err != nil {
		return fmt.Errorf("chmod +x %q: %w", filePath, err)
	}
	return nil
}
