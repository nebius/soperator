package consts

const (
	MariaDbDatabase       = "slurm_acct_db"
	MariaDbClusterSuffix  = "acct-db"
	MariaDbTable          = "slurm_acct_db.*"
	MariaDbUsername       = "slurm"
	MariaDbPasswordKey    = "password"
	MariaDbSecretName     = "mariadb-password"
	MariaDbSecretRootName = "mariadb-root"
	MariaDbPort           = 3306
	// MariaDbMyCnfTemplate is a fmt template; the verb is the innodb_buffer_pool_size value in MiB.
	MariaDbMyCnfTemplate = `[mariadb]
bind-address=*
default_storage_engine=InnoDB
innodb_default_row_format=DYNAMIC
innodb_buffer_pool_size=%dM
innodb_log_file_size=64M
innodb_lock_wait_timeout=900
max_allowed_packet=16M`
)
