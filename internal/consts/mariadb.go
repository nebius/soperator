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
	MyCnf                 = `[mariadb]
bind-address=*
default_storage_engine=InnoDB
innodb_default_row_format=DYNAMIC
innodb_buffer_pool_size=4096M
innodb_log_file_size=64M
innodb_lock_wait_timeout=900
max_allowed_packet=16M`
)
