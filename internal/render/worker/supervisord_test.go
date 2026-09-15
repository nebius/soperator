package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundledSupervisordConfig(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", "..", "images", "worker", "supervisord.conf"))
	require.NoError(t, err)

	rendered := string(config)
	assert.Contains(t, rendered, "command=/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config")
	assert.Contains(t, rendered, "command=/opt/bin/slurm/dockerd_entrypoint.sh chroot /mnt/jail /usr/bin/dockerd")
	assert.Contains(t, rendered, "command=/opt/bin/slurm/docker_proxy_entrypoint.sh worker")
	assert.NotContains(t, rendered, "docker_proxy_nginx_entrypoint.sh")
}
