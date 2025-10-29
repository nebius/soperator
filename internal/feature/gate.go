package feature

import (
	"k8s.io/component-base/featuregate"
)

var Gate = featuregate.NewFeatureGate()

func init() {
	_ = Gate.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		NodeSetWorkers: {
			Default:    false,
			PreRelease: featuregate.PreAlpha,
			//
			// TODO: Lock when PreRelease is GA.
			//LockToDefault: true,
		},
	})
}
