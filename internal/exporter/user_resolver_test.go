package exporter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserResolver_GetUserMap(t *testing.T) {
	// Create a temporary passwd file for testing
	tempDir := t.TempDir()
	passwdFile := filepath.Join(tempDir, "passwd")

	passwdContent := `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bin:x:2:2:bin:/bin:/usr/sbin/nologin
testuser:x:1001:1001:Test User:/home/testuser:/bin/bash
invalidline
validuser:x:1002:1002:Valid User:/home/validuser:/bin/bash
`

	err := os.WriteFile(passwdFile, []byte(passwdContent), 0644)
	require.NoError(t, err)

	resolver := NewUserResolverWithPasswdPath(passwdFile)

	t.Run("get complete user map", func(t *testing.T) {
		userMap, err := resolver.GetUserMap()
		require.NoError(t, err)

		expectedUsers := map[int32]string{
			0:    "root",
			1:    "daemon",
			2:    "bin",
			1001: "testuser",
			1002: "validuser",
		}

		assert.Equal(t, expectedUsers, userMap)
	})

	t.Run("empty passwd file", func(t *testing.T) {
		emptyFile := filepath.Join(tempDir, "empty_passwd")
		err := os.WriteFile(emptyFile, []byte(""), 0644)
		require.NoError(t, err)

		emptyResolver := NewUserResolverWithPasswdPath(emptyFile)
		userMap, err := emptyResolver.GetUserMap()
		require.NoError(t, err)
		assert.Empty(t, userMap)
	})

	t.Run("passwd file not found", func(t *testing.T) {
		resolverWithBadPath := NewUserResolverWithPasswdPath("/nonexistent/passwd")
		userMap, err := resolverWithBadPath.GetUserMap()
		assert.Error(t, err)
		assert.Nil(t, userMap)
		assert.Contains(t, err.Error(), "open passwd file")
	})
}
