package sconfigcontroller

import (
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

func (s *FileStore) Add(name, content, subPath string) error {
	dirPath := filepath.Join(s.path, subPath)

	if err := ensureDir(dirPath); err != nil {
		return err
	}

	file, err := os.Create(fmt.Sprintf("%s/%s", dirPath, name))
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	defer file.Close()

	if _, err = file.Write([]byte(content)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
