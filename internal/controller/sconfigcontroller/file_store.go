package sconfigcontroller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const direntCacheTTL = 15 * time.Second
const delayBetweenWriteAndReconfigure = direntCacheTTL + 1*time.Second

type FileStore struct {
	path string
}

var _ Store = &FileStore{}

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
	// In case this has to work on system without `renameat2` (or equivalent), it can be implemented with os.Link:
	// generate random name, call os.Link, loop if error is "already exists"
	// See os.CreateTemp implementation
	err = renameExchange(tempFileName, filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have just created tempFileName, so it's most probably about filePath, we can just rename it
			// Using renameNoReplace to avoid replacing file created after renameExchange but before this
			// But at the same time there's no cached dirent, so it should be safe to just rename it and move on
			err = renameNoReplace(tempFileName, filePath)
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

	// renameExchange has succeeded, and from this moment caches are stale, and we can't just remove temp file
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

func (s *FileStore) Write(filePath string, content []byte) (err error) {
	fullPath := filepath.Join(s.path, filePath)
	baseName := filepath.Base(filePath)
	dirPath := filepath.Dir(fullPath)

	if err = ensureDir(dirPath); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dirPath, baseName)
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
	// In case this has to work on system without `renameat2` (or equivalent), it can be implemented with os.Link:
	// generate random name, call os.Link, loop if error is "already exists"
	// See os.CreateTemp implementation
	err = renameExchange(tempFileName, fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have just created tempFileName, so it's most probably about fullPath, we can just rename it
			// Using renameNoReplace to avoid replacing file created after renameExchange but before this
			// But at the same time there's no cached dirent, so it should be safe to just rename it and move on
			err = renameNoReplace(tempFileName, fullPath)
			if err != nil {
				// If rename failed we can just delete temp file and move on
				err = fmt.Errorf("rename temp to target file (%s => %s): %w", tempFileName, fullPath, err)
			}
			deferTempFileRemove = false
			return err
		}

		// If exchange failed we can just delete temp file and move on
		err = fmt.Errorf("switch temp and target files (%s and %s): %w", tempFileName, fullPath, err)
		return err
	}

	// Some filesystems can keep directory entries caches for too long without invalidating
	// This delay is expected to make these caches stale on worker VMs
	// So when slurmd will restart, it will pick up new inodes for config files
	// Also it should not keep inodes for old files alive, because only directory entries are cached, not inodes themselves
	// Deleting earlier can lead to "file not found" errors

	// renameExchange has succeeded, and from this moment caches are stale, and we can't just remove temp file
	// without triggering errors on readers
	deferTempFileRemove = false
	// TODO sleep do this once per reconciliation, will be done in #1200
	time.Sleep(delayBetweenWriteAndReconfigure)

	// Delete old entry and free old inode, now that cache is invalidated it should be safe
	err = os.Remove(tempFileName)

	return err
}

func (s *FileStore) Chmod(path string, mode uint32) error {
	filePath := filepath.Join(s.path, path)

	if err := os.Chmod(filePath, os.FileMode(mode)); err != nil {
		return fmt.Errorf("chmod %q %#o: %w", filePath, mode, err)
	}
	return nil
}

type replacedFile struct {
	targetName string
	tempName   string
}

// ReplacedFilesBatch is used to batch files replacement together and wait for dirent caches invalidation just once
type ReplacedFilesBatch struct {
	pendingFiles []replacedFile
}

func NewReplacedFilesBatch() *ReplacedFilesBatch {
	return &ReplacedFilesBatch{}
}

func (batch *ReplacedFilesBatch) Replace(filePath string, content []byte, mode uint32) (err error) {
	baseName := filepath.Base(filePath)
	dirPath := filepath.Dir(filePath)

	// TODO is it necessary here?
	if err = ensureDir(dirPath); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dirPath, baseName)
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

	if _, err = tempFile.Write(content); err != nil {
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

	err = os.Chmod(tempFileName, os.FileMode(mode))
	if err != nil {
		err = fmt.Errorf("chmod temp file: %w", err)
		return err
	}

	// Don't just rename in case of dirent caches
	// In case this has to work on system without `renameat2` (or equivalent), it can be implemented with os.Link:
	// generate random name, call os.Link, loop if error is "already exists"
	// See os.CreateTemp implementation
	err = renameExchange(tempFileName, filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have just created tempFileName, so it's most probably about fullPath, we can just rename it
			// Using renameNoReplace to avoid replacing file created after renameExchange but before this
			// But at the same time there's no cached dirent, so it should be safe to just rename it and move on
			err = renameNoReplace(tempFileName, filePath)
			if err != nil {
				// If rename failed we can just delete temp file and move on
				err = fmt.Errorf("rename temp to target file (%s => %s): %w", tempFileName, filePath, err)
			}
			deferTempFileRemove = false

			// Because there were no "old" file to begin with, it can leave it present on FS even after clean up
			// * There were no old file => no old inode => nothing should be cached
			// * No temp file on FS => nothing to remove on FS
			// So, nothing to append to pendingFiles
			return err
		}

		// If exchange failed we can just delete temp file and move on
		err = fmt.Errorf("switch temp and target files (%s and %s): %w", tempFileName, filePath, err)
		return err
	}

	// Some filesystems can keep directory entries caches for too long without invalidating
	// This delay is expected to make these caches stale on worker VMs
	// So when slurmd will restart, it will pick up new inodes for config files
	// Also it should keep inodes for old files alive while caches are alive, because only directory entries are cached, not inodes themselves
	// Deleting earlier can lead to "file not found" errors

	// renameExchange has succeeded, and from this moment caches are stale, and we can't just remove temp file
	// without triggering errors on readers
	deferTempFileRemove = false

	batch.pendingFiles = append(batch.pendingFiles, replacedFile{
		targetName: filePath,
		tempName:   tempFileName,
	})

	return err
}

func (batch *ReplacedFilesBatch) Cleanup() error {
	// All present files in the bunch were already replaced, and were waiting for other files and/or for cache invalidation
	// To avoid caching issues it should keep old files, which are at the moment pointed to by temp names
	// Also, to clean up, it should remove temp file names from FS
	// So this has to replace olds files back to target names, and only then remove temp file paths

	errs := make([]error, 0)
	for _, file := range batch.pendingFiles {
		err := renameNoReplace(file.tempName, file.targetName)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = os.Remove(file.tempName)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	// There's no pending files now, next cleanup shouldn't do anything
	batch.pendingFiles = nil

	return errors.Join(errs...)
}

func (batch *ReplacedFilesBatch) Finish() error {
	// renameExchange has succeeded, but caches are stale, and we can't just remove temp file
	// without triggering errors on readers
	time.Sleep(delayBetweenWriteAndReconfigure)

	// Now caches should be invalidated, and old files are not needed
	// Removing temp paths from FS
	errs := make([]error, 0)
	for _, file := range batch.pendingFiles {
		err := os.Remove(file.tempName)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	// There's no pending files now, next cleanup shouldn't do anything
	batch.pendingFiles = nil

	return errors.Join(errs...)
}
