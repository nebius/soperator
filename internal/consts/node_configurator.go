package consts

const (
	RebooterMethodEnv   = "REBOOTER_EVICTION_METHOD"
	RebooterNodeNameEnv = "REBOOTER_NODE_NAME"
)

type RebooterMethod string

const (
	RebooterEvict RebooterMethod = "evict"
	RebooterDrain RebooterMethod = "drain"
)

const (
	NodeConfiguratorName = "node-configurator"
)
