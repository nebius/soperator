package jwt

import (
	"time"

	"istio.io/pkg/cache"
)

// TokenRegistry represents an in-memory token cache with customizable expiration, eviction period, and maximum entries.
type TokenRegistry struct {
	cache cache.ExpiringCache

	expirationPeriod time.Duration
	evictionPeriod   time.Duration
	maxEntries       int32
}

// NewTokenRegistry creates a new TokenRegistry with default configuration values.
func NewTokenRegistry() *TokenRegistry {
	return &TokenRegistry{
		expirationPeriod: DefaultTokenLifetime - DefaultTokenEviction,
		evictionPeriod:   DefaultTokenEviction,
		maxEntries:       DefaultMaxCacheEntries,
	}
}

// WithExpirationPeriod sets a custom expiration period for tokens in the registry.
func (r *TokenRegistry) WithExpirationPeriod(period time.Duration) *TokenRegistry {
	r.expirationPeriod = period
	return r
}

// WithEvictionPeriod sets a custom eviction period for removing expired tokens.
func (r *TokenRegistry) WithEvictionPeriod(period time.Duration) *TokenRegistry {
	r.evictionPeriod = period
	return r
}

// WithMaxEntries sets the maximum number of entries allowed in the token registry.
func (r *TokenRegistry) WithMaxEntries(maxEntries int32) *TokenRegistry {
	r.maxEntries = maxEntries
	return r
}

// Build initializes the token registry with the specified configuration.
func (r *TokenRegistry) Build() *TokenRegistry {
	r.cache = cache.NewLRU(
		r.expirationPeriod,
		r.evictionPeriod,
		r.maxEntries,
	)

	return r
}

// Register adds a token to the registry with a given key and default expiration.
func (r *TokenRegistry) Register(key string, token string) {
	r.cache.Set(key, token)
}

// RegisterWithLifetime adds a token with a custom expiration duration.
func (r *TokenRegistry) RegisterWithLifetime(key string, token string, lifetime time.Duration) {
	r.cache.SetWithExpiration(key, token, lifetime)
	r.cache.EvictExpired()
}

// Get retrieves a token from the registry by its key.
// Returns the token if found and a boolean indicating success.
func (r *TokenRegistry) Get(key string) (string, bool) {
	r.cache.EvictExpired()

	v, found := r.cache.Get(key)
	if !found {
		return "", false
	}

	token, ok := v.(string)
	if !ok {
		return "", false
	}

	return token, true
}
