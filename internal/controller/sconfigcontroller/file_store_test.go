package sconfigcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileStore_Add(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		path        string
		fileName    string
		content     string
		subPath     string
		expectError bool
	}{
		{
			name:        "successful write",
			path:        tempDir,
			fileName:    "testfile.txt",
			content:     "content",
			expectError: false,
		},
		{
			name:        "invalid directory",
			path:        "/invalid/path",
			fileName:    "testfile.txt",
			content:     "content",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewFileStore(tt.path)
			err := fs.Add(tt.fileName, tt.content, tt.subPath)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.expectError {
				// Verify the file content
				filePath := filepath.Join(tt.path+tt.subPath, tt.fileName)
				content, err := os.ReadFile(filePath)

				require.NoError(t, err)
				require.Equal(t, tt.content, string(content))
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		dirPath     string
		setup       func(string) error
		expectError bool
		checkAfter  func(string) error
	}{
		{
			name:        "create new directory",
			dirPath:     filepath.Join(tempDir, "new_dir"),
			expectError: false,
			checkAfter: func(path string) error {
				if stat, err := os.Stat(path); err != nil {
					return fmt.Errorf("directory should exist: %w", err)
				} else if !stat.IsDir() {
					return fmt.Errorf("path should be a directory")
				}
				return nil
			},
		},
		{
			name:        "create nested directories",
			dirPath:     filepath.Join(tempDir, "level1", "level2", "level3"),
			expectError: false,
			checkAfter: func(path string) error {
				if stat, err := os.Stat(path); err != nil {
					return fmt.Errorf("nested directory should exist: %w", err)
				} else if !stat.IsDir() {
					return fmt.Errorf("path should be a directory")
				}
				return nil
			},
		},
		{
			name:    "directory already exists",
			dirPath: filepath.Join(tempDir, "existing_dir"),
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
			expectError: false,
			checkAfter: func(path string) error {
				if stat, err := os.Stat(path); err != nil {
					return fmt.Errorf("existing directory should still exist: %w", err)
				} else if !stat.IsDir() {
					return fmt.Errorf("path should still be a directory")
				}
				return nil
			},
		},
		{
			name:        "empty path",
			dirPath:     "",
			expectError: true,
		},
		{
			name:        "root directory",
			dirPath:     "/",
			expectError: false,
		},
		{
			name:        "current directory",
			dirPath:     ".",
			expectError: false,
		},
		{
			name:        "parent directory reference",
			dirPath:     "..",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(tt.dirPath); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			}

			err := ensureDir(tt.dirPath)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}

			if !tt.expectError && tt.checkAfter != nil {
				if checkErr := tt.checkAfter(tt.dirPath); checkErr != nil {
					t.Errorf("post-test check failed: %v", checkErr)
				}
			}
		})
	}
}

func TestFileStore_SetExecutable(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		setup       func(fs *FileStore) (fileName, subPath string, err error)
		expectError bool
		check       func(path string) error
	}{
		{
			name: "make file executable",
			setup: func(fs *FileStore) (string, string, error) {
				fileName := "script.sh"
				subPath := "bin"
				err := fs.Add(fileName, "#!/bin/bash\necho test", subPath)
				return fileName, subPath, err
			},
			expectError: false,
			check: func(path string) error {
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				mode := info.Mode().Perm()
				if mode&0o111 == 0 {
					return fmt.Errorf("file is not executable: mode %o", mode)
				}
				return nil
			},
		},
		{
			name: "file does not exist",
			setup: func(fs *FileStore) (string, string, error) {
				return "nonexistent.sh", "", nil
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewFileStore(tempDir)
			fileName, subPath, err := tt.setup(fs)
			require.NoError(t, err)

			err = fs.SetExecutable(fileName, subPath)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				fullPath := filepath.Join(tempDir, subPath, fileName)
				if tt.check != nil {
					require.NoError(t, tt.check(fullPath))
				}
			}
		})
	}
}
