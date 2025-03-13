package jwt_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/pkg/jwt"
)

func TestTokenRegistry_RegisterAndGet(t *testing.T) {
	registry := jwt.NewTokenRegistry().Build()
	key := "test-key"
	token := "test-token"

	registry.Register(key, token)
	retrievedToken, found := registry.Get(key)

	assert.True(t, found, "Token should be found")
	assert.Equal(t, token, retrievedToken, "Retrieved token should match the registered token")
}

func TestTokenRegistry_RegisterWithLifetime(t *testing.T) {
	registry := jwt.NewTokenRegistry().Build()
	key := "test-key-lifetime"
	token := "test-token-lifetime"
	lifetime := time.Second * 2

	registry.RegisterWithLifetime(key, token, lifetime)

	// Token should be available immediately
	retrievedToken, found := registry.Get(key)
	assert.True(t, found, "Token should be found immediately")
	assert.Equal(t, token, retrievedToken, "Retrieved token should match the registered token")

	// Wait for the token to expire
	time.Sleep(lifetime + time.Second)

	_, found = registry.Get(key)
	assert.False(t, found, "Token should have expired and not be found")
}

func TestTokenRegistry_TokenExpiration(t *testing.T) {
	registry := jwt.NewTokenRegistry().
		WithExpirationPeriod(time.Second * 1).
		Build()
	key := "test-expiration"
	token := "expiring-token"

	registry.Register(key, token)

	// Token should be available immediately
	retrievedToken, found := registry.Get(key)
	assert.True(t, found, "Token should be found immediately")
	assert.Equal(t, token, retrievedToken, "Retrieved token should match the registered token")

	// Wait for the token to expire
	time.Sleep(time.Second * 2)

	_, found = registry.Get(key)
	assert.False(t, found, "Token should have expired and not be found")
}

func TestTokenRegistry_MaxEntries(t *testing.T) {
	registry := jwt.NewTokenRegistry().
		WithMaxEntries(2).
		Build()

	registry.Register("key1", "token1")
	registry.Register("key2", "token2")
	registry.Register("key3", "token3") // This should evict the oldest entry (key1)

	_, found := registry.Get("key1")
	assert.False(t, found, "Oldest token (key1) should be evicted")

	token2, found := registry.Get("key2")
	assert.True(t, found, "Token for key2 should still exist")
	assert.Equal(t, "token2", token2)

	token3, found := registry.Get("key3")
	assert.True(t, found, "Token for key3 should still exist")
	assert.Equal(t, "token3", token3)
}
