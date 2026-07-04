package topologyconfcontroller

// maxSafeTrailingDigits is the longest trailing decimal-digit run a switch or block name may end
// with before Slurm's hostlist parser risks overflowing it. Slurm decomposes every name into a
// prefix plus a trailing contiguous decimal run and stores that run as a uint64. UINT64_MAX
// (18446744073709551615) is 20 digits; runs of 19 digits always fit (max 9_999_999_999_999_999_999
// < UINT64_MAX), so 20+ digit runs are the real risk. We keep one extra digit of margin below the
// boundary and treat any run longer than 18 digits as unsafe.
const maxSafeTrailingDigits = 18

// slurmNameTerminator is appended to unsafe names. '_' never appears in hex IB-fabric switch IDs
// and carries no special meaning to Slurm's hostlist syntax, so it cannot collide with a real name
// or be misparsed.
const slurmNameTerminator = "_"

// slurmSafeSwitchName makes a switch or block name safe to emit in topology.conf.
//
// Slurm only numerically parses the *trailing* decimal run of a name; when that run is long enough
// to exceed the uint64 range Slurm saturates it to UINT64_MAX and rewrites the name, so a parent's
// "Switches=" child reference no longer matches the child's "SwitchName=" line and slurmctld dies
// with "has invalid child". Appending a non-digit terminator empties the trailing run, so Slurm
// stores the name verbatim. Applying this consistently to a switch both where it is declared and
// where it is referenced keeps the two in sync.
//
// It must never be applied to worker node names: those must match real Slurm node names exactly.
func slurmSafeSwitchName(name string) string {
	if trailingDigitCount(name) <= maxSafeTrailingDigits {
		return name
	}
	return name + slurmNameTerminator
}

// trailingDigitCount returns the length of the trailing contiguous run of ASCII decimal digits.
func trailingDigitCount(name string) int {
	count := 0
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] < '0' || name[i] > '9' {
			break
		}
		count++
	}
	return count
}
