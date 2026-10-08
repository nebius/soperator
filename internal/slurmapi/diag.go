package slurmapi

// Diag contains the controller statistics used by the exporter. Slurm's RPC
// counters and user IDs can exceed the signed int32 fields in the generated SDK.
type Diag struct {
	Statistics Statistics `json:"statistics"`
}

type Statistics struct {
	ServerThreadCount *int32       `json:"server_thread_count"`
	ScheduleCycleSum  *uint64      `json:"schedule_cycle_sum"`
	RpcsByMessageType *[]RPCByType `json:"rpcs_by_message_type"`
	RpcsByUser        *[]RPCByUser `json:"rpcs_by_user"`
}

type RPCByType struct {
	MessageType string `json:"message_type"`
	Count       uint64 `json:"count"`
	TotalTime   uint64 `json:"total_time"`
}

type RPCByUser struct {
	User      string `json:"user"`
	UserId    uint32 `json:"user_id"`
	Count     uint64 `json:"count"`
	TotalTime uint64 `json:"total_time"`
}
