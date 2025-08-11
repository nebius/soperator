package sconfigcontroller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type replacedFile struct {
	targetName string
	tempName   string
}

// ReplacedFilesBatch is used to batch files replacement together and wait for dirent caches invalidation just once
type ReplacedFilesBatch struct {
	fs           Fs
	pendingFiles []replacedFile
}

func NewReplacedFilesBatch(fs Fs) *ReplacedFilesBatch {
	return &ReplacedFilesBatch{
		fs:           fs,
		pendingFiles: nil,
	}
}

func (batch *ReplacedFilesBatch) Replace(filePath string, content []byte, mode os.FileMode) (err error) {
	dirPath := filepath.Dir(filePath)

	err = batch.fs.MkdirAll(dirPath, 0o755)
	if err != nil {
		err = fmt.Errorf("preparing dir for file: %w", err)
		return err
	}

	tempFileName, err := batch.fs.PrepareNewFile(filePath, content, mode)
	if err != nil {
		err = fmt.Errorf("preparing new file: %w", err)
		return err
	}

	deferTempFileRemove := true

	defer func() {
		if deferTempFileRemove {
			// Remove left-over temp file
			err = errors.Join(err, batch.fs.Remove(tempFileName))
		}
	}()

	// Don't just rename in case of dirent caches
	// In case this has to work on system without `renameat2` (or equivalent), it can be implemented with os.Link:
	// generate random name, call os.Link, loop if error is "already exists"
	// See os.CreateTemp implementation
	err = batch.fs.RenameExchange(tempFileName, filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We have just created tempFileName, so error is most probably about fullPath, we can just rename it
			// Using RenameNoReplace to avoid replacing file created after RenameExchange but before this
			// But at the same time there's no cached dirent for old file, so it should be safe to just rename it and move on
			err = batch.fs.RenameNoReplace(tempFileName, filePath)
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
	// So this has to replace old files back to target names, and only then remove temp file paths

	errs := make([]error, 0)
	for _, file := range batch.pendingFiles {
		err := batch.fs.RenameExchange(file.tempName, file.targetName)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = batch.fs.Remove(file.tempName)
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
	if len(batch.pendingFiles) == 0 {
		// There were no changes in files => nothing to sync
		return nil
	}

	// renameExchange has succeeded, but caches are stale, and we can't just remove temp file
	// without triggering errors on readers
	err := batch.fs.SyncCaches()
	if err != nil {
		// At this moment it's not clear if it is safe to clean up temp files or not
		// Just assuming there's nothing to clean up
		batch.pendingFiles = nil
		return err
	}

	// Now caches should be invalidated, and old files are not needed
	// Removing temp paths from FS
	errs := make([]error, 0)
	for _, file := range batch.pendingFiles {
		err := batch.fs.Remove(file.tempName)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	// There's no pending files now, next cleanup shouldn't do anything
	batch.pendingFiles = nil

	return errors.Join(errs...)
}
