package consts

const (
	SecretMungeKeyName     = Munge
	SecretMungeKeyFileName = Munge + ".key"
	SecretMungeKeyFileMode = int32(0400)

	SecretRESTJWTKeyFileName = "rest_jwt.key"
	SecretRESTJWTKeyFileMode = int32(0400)

	SecretSshdKeysPrivateFileMode  = int32(0600)
	SecretSshdKeysPublicFileMode   = int32(0644)
	SecretSshdKeysName             = "sshd-keys"
	SecretSshdPublicKeysPostfix    = ".pub"
	SecretSshdECDSAKeyName         = "ssh_host_ecdsa_key"
	SecretSshdECDSAPubKeyName      = SecretSshdECDSAKeyName + SecretSshdPublicKeysPostfix
	SecretSshdECDSA25519KeyName    = "ssh_host_ed25519_key"
	SecretSshdECDSA25519PubKeyName = SecretSshdECDSA25519KeyName + SecretSshdPublicKeysPostfix
	SecretSshdRSAKeyName           = "ssh_host_rsa_key"
	SecretSshdRSAPubKeyName        = SecretSshdRSAKeyName + SecretSshdPublicKeysPostfix

	SecretSlurmdbdSSLServerCACertificateFile  = "ca.crt"
	SecretSlurmdbdSSLClientKeyPrivateKeyFile  = "tls.key"
	SecretSlurmdbdSSLClientKeyCertificateFile = "tls.crt"
	SecretSlurmdbdSSLClientKeyFileMode        = int32(0400)
	SecretSlurmdbdSSLServerCAFileMode         = int32(0400)
)
