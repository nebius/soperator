package consts

// https://linux.die.net/man/5/sshd_config

const (
	SSHDClientAliveInterval = "9000" // 30 minute
	SSHDClientAliveCountMax = "10"
	SSHDMaxStartups         = "10:30:60"
	SSHDLoginGraceTime      = "9000"
	SSHDMaxAuthTries        = "4"
)
