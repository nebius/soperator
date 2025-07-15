package sconfigcontroller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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

	file, err := os.Create(filePath)
	if err != nil {
		err = fmt.Errorf("open file: %w", err)
		return err
	}

	defer func() {
		// Errors from file.Close() are especially important on NFS/virtiofs/... for close-to-open sync
		// man 5 nfs
		// > Close-to-open cache consistency
		// > ...
		// > When the application closes the file, the NFS client writes back
		// > any pending changes to the file so that the next opener can view
		// > the changes.  This also gives the NFS client an opportunity to
		// > report write errors to the application via the return code from
		// > close(2).
		err = errors.Join(err, file.Close())
	}()

	if _, err = file.Write([]byte(content)); err != nil {
		err = fmt.Errorf("write file: %w", err)
		return err
	}

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
