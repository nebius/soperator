package consts

import "errors"

type NCCLType interface {
	ncclType()
	String() string
}

type baseNCCLType struct {
	value string
}

func (b baseNCCLType) ncclType() {}
func (b baseNCCLType) String() string {
	return b.value
}

var (
	NCCLTypeAuto           NCCLType = baseNCCLType{"auto"}
	NCCLTypeH100GPUCluster NCCLType = baseNCCLType{"H100 GPU cluster"}
	NCCLTypeCustom         NCCLType = baseNCCLType{"custom"}
)

var ncclTypeMap = map[string]NCCLType{
	"auto":             NCCLTypeAuto,
	"H100 GPU cluster": NCCLTypeH100GPUCluster,
	"custom":           NCCLTypeCustom,
}

func StringToNCCLType(s string) (NCCLType, error) {
	if val, ok := ncclTypeMap[s]; ok {
		return val, nil
	}
	return nil, errors.New("unknown NCCLType: " + s)
}
