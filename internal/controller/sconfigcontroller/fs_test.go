package sconfigcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefixFs_MkdirAll(t *testing.T) {
	tempDir := t.TempDir()
	fs := PrefixFs{Prefix: tempDir}

	tests := []struct {
		name        string
		dirPath     string
		setup       func(string) error
		expectError string
		checkAfter  func(string) error
	}{
		{
			name:    "create new directory",
			dirPath: filepath.Join("/", "new_dir"),
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
			name:    "create nested directories",
			dirPath: filepath.Join("/", "level1", "level2", "level3"),
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
				return os.MkdirAll(path, 0o755)
			},
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
			expectError: "path is not absolute",
		},
		{
			name:    "root directory",
			dirPath: "/",
		},
		{
			name:        "current directory",
			dirPath:     ".",
			expectError: "path is not absolute",
		},
		{
			name:        "parent directory reference",
			dirPath:     "..",
			expectError: "path is not absolute",
		},
		{
			name:        "relative path",
			dirPath:     "rel_dir",
			expectError: "path is not absolute",
		},
		{
			name:        "path traversal leads to root",
			dirPath:     "/foo/.././../bar/../../baz",
			expectError: "path traversal is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath := filepath.Join(tempDir, tt.dirPath)
			if tt.setup != nil {
				if err := tt.setup(fullPath); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			}

			err := fs.MkdirAll(tt.dirPath, 0o755)

			// Check error expectation
			if tt.expectError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.expectError)
			}

			if tt.expectError == "" && tt.checkAfter != nil {
				if checkErr := tt.checkAfter(fullPath); checkErr != nil {
					t.Errorf("post-test check failed: %v", checkErr)
				}
			}
		})
	}
}

func TestPrefixFs_PrepareNewFile(t *testing.T) {
	tempDir := t.TempDir()
	fmt.Println(tempDir)
	fs := PrefixFs{Prefix: tempDir}

	content := []byte{1, 2, 3}
	mode := os.FileMode(0o644)

	tests := []struct {
		name              string
		fileName          string
		expectError       string
		checkTempFileName func(t *testing.T, tempFile string)
	}{
		{
			name:     "successful write",
			fileName: "/testfile.txt",
		},
		{
			name:        "relative path",
			fileName:    "testfile.txt",
			expectError: "path is not absolute",
		},
		{
			name:        "special file name",
			fileName:    "/testfile * more stars * *.txt",
			expectError: "",
			checkTempFileName: func(t *testing.T, tempFile string) {
				require.Contains(t, tempFile, "/testfile * more stars * *.txt")
			},
		},
		{
			name:        "path traversal",
			fileName:    "/foo/.././../bar/../../baz",
			expectError: "path traversal is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempFile, err := fs.PrepareNewFile(tt.fileName, content, mode)

			if tt.expectError != "" {
				require.ErrorContains(t, err, tt.expectError)
			} else {
				require.NoError(t, err)
			}

			if tt.expectError == "" {
				// Verify that returned filename does not contain prefix
				require.NotContains(t, tempFile, tempDir)
				if tt.checkTempFileName != nil {
					tt.checkTempFileName(t, tempFile)
				}

				// Verify the file content
				filePath := filepath.Join(tempDir, tempFile)
				actualContent, err := os.ReadFile(filePath)

				require.NoError(t, err)
				require.Equal(t, content, actualContent)
			}
		})
	}
}
