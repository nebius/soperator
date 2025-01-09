package consts

// https://linux.die.net/man/5/sshd_config

const (
	SSHDClientAliveInterval = "3600" // 1 hour
	SSHDClientAliveCountMax = "5"
	SSHDMaxStartups         = "100:50:300"
	SSHDLoginGraceTime      = "120"
	SSHDMaxAuthTries        = "4"
)
