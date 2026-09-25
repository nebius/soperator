package controllerconfig

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

// LeaderElectionConfig reserves an independent API budget for lease renewals.
func LeaderElectionConfig(config *rest.Config) *rest.Config {
	leaderConfig := rest.CopyConfig(config)
	leaderConfig.QPS = rest.DefaultQPS
	leaderConfig.Burst = rest.DefaultBurst
	leaderConfig.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(leaderConfig.QPS, leaderConfig.Burst)
	return leaderConfig
}
