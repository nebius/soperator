package exporter

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// UserResolver provides user ID to username resolution by parsing passwd files.
type UserResolver struct {
	passwdPath string
}

// NewUserResolver creates a new UserResolver.
func NewUserResolver() *UserResolver {
	return &UserResolver{
		passwdPath: "/mnt/jail-etc/passwd",
	}
}

// NewUserResolverWithPasswdPath creates a new UserResolver with a custom passwd file path.
// This is primarily used for testing.
func NewUserResolverWithPasswdPath(passwdPath string) *UserResolver {
	return &UserResolver{
		passwdPath: passwdPath,
	}
}

// GetUserMap parses the passwd file and returns a complete map of user ID to username.
func (r *UserResolver) GetUserMap() (map[int32]string, error) {
	file, err := os.Open(r.passwdPath)
	if err != nil {
		return nil, fmt.Errorf("open passwd file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	userMap := make(map[int32]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		// passwd format: username:password:uid:gid:gecos:home:shell
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}

		uid, err := strconv.ParseInt(parts[2], 10, 32)
		if err != nil {
			continue
		}

		userMap[int32(uid)] = parts[0]
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan passwd file: %w", err)
	}

	return userMap, nil
}
