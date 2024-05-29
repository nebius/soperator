package consts

const (
	SecretSlurmKeyName = SlurmPrefix + "key"

	// SecretSlurmKeySlurmKeyKey has such a fascinating name 🔥.
	// It's a key in a secret SecretSlurmKeyName which holds Slurm key file 'slurm.key' contents
	SecretSlurmKeySlurmKeyKey = "slurm.key"

	SecretSlurmKeySlurmKeyPath = SecretSlurmKeySlurmKeyKey
)

var (
	// SecretSlurmKeySlurmKeyMode is a var in order to get its address
	SecretSlurmKeySlurmKeyMode = int32(0400)
)
