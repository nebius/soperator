package sconfigcontroller

import (
	"fmt"
	"os"
)

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{
		path: path,
	}
}

func (s *FileStore) Add(name string, content string) error {
	file, err := os.Create(fmt.Sprintf("%s/%s", s.path, name))
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	if _, err = file.Write([]byte(content)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("closing file: %w", err)
	}

	return nil
}
