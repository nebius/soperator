package jwt

import (
	"nebius.ai/slurm-operator/internal/jwt"
)

type (
	Token         = jwt.Token
	TokenRegistry = jwt.TokenRegistry
)

var (
	NewToken         = jwt.NewToken
	NewTokenRegistry = jwt.NewTokenRegistry

	GenerateSigningKey = jwt.GenerateSigningKey
)

var (
	DefaultTokenLifetime = jwt.DefaultTokenLifetime
	DefaultTokenEviction = jwt.DefaultTokenEviction
)

const (
	DefaultMaxCacheEntries = jwt.DefaultMaxCacheEntries
)
