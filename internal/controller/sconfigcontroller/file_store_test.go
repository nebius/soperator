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
			subPath:     "",
			expectError: false,
		},
		{
			name:        "invalid directory",
			path:        "/invalid/path",
			fileName:    "testfile.txt",
			content:     "content",
			subPath:     "/kek",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewFileStore(tt.path + tt.subPath)
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
