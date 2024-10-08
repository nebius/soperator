package consts

import "errors"

type ClusterType interface {
	ClusterType()
	String() string
}

type baseClusterType struct {
	value string
}

func (b baseClusterType) ClusterType() {}
func (b baseClusterType) String() string {
	return b.value
}

var (
	ClusterTypeGPU ClusterType = baseClusterType{"gpu"}
	ClusterTypeCPU ClusterType = baseClusterType{"cpu"}
)

var clusterTypeMap = map[string]ClusterType{
	"gpu": ClusterTypeGPU,
	"cpu": ClusterTypeCPU,
}

func StringToClusterType(s string) (ClusterType, error) {
	if val, ok := clusterTypeMap[s]; ok {
		return val, nil
	}
	return nil, errors.New("unknown ClusterType: " + s)
}
