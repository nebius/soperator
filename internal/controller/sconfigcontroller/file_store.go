package sconfigcontroller

import (
	"fmt"
	"io"
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

func (s *FileStore) Open(name string) (io.Writer, error) {
	file, err := os.Create(fmt.Sprintf("%s/%s", s.path, name))
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	return file, nil
}
