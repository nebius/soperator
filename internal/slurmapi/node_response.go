package slurmapi

import api "github.com/SlinkyProject/slurm-client/api/v0044"

// nodeInfo excludes unused scheduling metadata such as weight, whose unsigned
// range exceeds the SDK's int32 field.
type nodeInfo struct {
	Address           *string                `json:"address,omitempty"`
	AllocCpus         *int32                 `json:"alloc_cpus,omitempty"`
	AllocIdleCpus     *int32                 `json:"alloc_idle_cpus,omitempty"`
	AllocMemory       *int64                 `json:"alloc_memory,omitempty"`
	BootTime          *optionalNumber[int64] `json:"boot_time,omitempty"`
	ClusterName       *string                `json:"cluster_name,omitempty"`
	Comment           *string                `json:"comment,omitempty"`
	Cpus              *int32                 `json:"cpus,omitempty"`
	EffectiveCpus     *int32                 `json:"effective_cpus,omitempty"`
	FreeMem           *optionalNumber[int64] `json:"free_mem,omitempty"`
	InstanceId        *string                `json:"instance_id,omitempty"`
	Name              *string                `json:"name,omitempty"`
	Partitions        *api.V0044CsvString    `json:"partitions,omitempty"`
	RealMemory        *int64                 `json:"real_memory,omitempty"`
	Reason            *string                `json:"reason,omitempty"`
	ReasonChangedAt   *optionalNumber[int64] `json:"reason_changed_at,omitempty"`
	Reservation       *string                `json:"reservation,omitempty"`
	SpecializedMemory *int64                 `json:"specialized_memory,omitempty"`
	State             *[]api.V0044NodeState  `json:"state,omitempty"`
	Tres              *string                `json:"tres,omitempty"`
}

func (n nodeInfo) apiNode() api.V0044Node {
	return api.V0044Node{
		Address:           n.Address,
		AllocCpus:         n.AllocCpus,
		AllocIdleCpus:     n.AllocIdleCpus,
		AllocMemory:       n.AllocMemory,
		BootTime:          toAPINumber64(n.BootTime),
		ClusterName:       n.ClusterName,
		Comment:           n.Comment,
		Cpus:              n.Cpus,
		EffectiveCpus:     n.EffectiveCpus,
		FreeMem:           toAPINumber64(n.FreeMem),
		InstanceId:        n.InstanceId,
		Name:              n.Name,
		Partitions:        n.Partitions,
		RealMemory:        n.RealMemory,
		Reason:            n.Reason,
		ReasonChangedAt:   toAPINumber64(n.ReasonChangedAt),
		Reservation:       n.Reservation,
		SpecializedMemory: n.SpecializedMemory,
		State:             n.State,
		Tres:              n.Tres,
	}
}

func toAPINumber64(n *optionalNumber[int64]) *api.V0044Uint64NoValStruct {
	if n == nil {
		return nil
	}
	return &api.V0044Uint64NoValStruct{Set: n.Set, Infinite: n.Infinite, Number: n.Number}
}
