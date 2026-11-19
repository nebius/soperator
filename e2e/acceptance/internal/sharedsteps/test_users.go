package sharedsteps

// Pro E2E exempts these direct-SSH accounts from PAM adoption. Keep their names
// in sync with pamAdoptExemptTestUsers in internal/e2e/values.go.
const (
	sshTestUserName   = "bob"
	dockerSSHUserName = "dockeruser"
)

// Adoption checks need separate, non-exempt accounts so their jobs and cleanup
// do not interfere with direct-SSH checks or each other.
const (
	pamSlurmAdoptUser       = "soperatoradopt"
	pamSlurmAdoptLimitsUser = "soperatoradoptlimits"
)
