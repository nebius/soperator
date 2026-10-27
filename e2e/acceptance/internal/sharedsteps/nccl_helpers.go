package sharedsteps

import "regexp"

var ncclCorrectLine = regexp.MustCompile(`^#\s*Out of bounds values\s*:\s*0\s+OK\s*$`)
