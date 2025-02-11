package jwt

import (
	"time"
)

var (
	DefaultTokenLifetime = time.Hour * 24 * 30
	DefaultTokenEviction = time.Hour * 24
)

const (
	SigningKeyLength = 32

	DefaultMaxCacheEntries = 10
)
