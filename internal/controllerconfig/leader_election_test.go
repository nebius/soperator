package controllerconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

func TestLeaderElectionHasIndependentAPIBudget(t *testing.T) {
	config := &rest.Config{
		Host:        "https://kubernetes.example",
		BearerToken: "test-token",
		QPS:         0.001,
		Burst:       1,
		RateLimiter: flowcontrol.NewTokenBucketRateLimiter(0.001, 1),
	}
	leaderConfig := LeaderElectionConfig(config)
	require.True(t, config.RateLimiter.TryAccept())
	require.False(t, config.RateLimiter.TryAccept())
	require.True(t, leaderConfig.RateLimiter.TryAccept())
	require.Equal(t, float32(rest.DefaultQPS), leaderConfig.QPS)
	require.Equal(t, rest.DefaultBurst, leaderConfig.Burst)
	require.Equal(t, config.Host, leaderConfig.Host)
	require.Equal(t, config.BearerToken, leaderConfig.BearerToken)
	require.Equal(t, float32(0.001), config.QPS)
	require.Equal(t, 1, config.Burst)
}
