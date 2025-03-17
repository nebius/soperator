package sconfigcontroller

import (
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
			err := fs.Add(tt.fileName, tt.content)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.expectError {
				// Verify the file content
				filePath := filepath.Join(tt.path, tt.fileName)
				content, err := os.ReadFile(filePath)

				require.NoError(t, err)
				require.Equal(t, tt.content, string(content))
			}
		})
	}
}
