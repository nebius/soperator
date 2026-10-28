package slurmapi

// Diag contains the controller statistics used by the exporter. RPC counters
// can exceed the signed int32 fields in the generated SDK.
type Diag struct {
	Statistics Statistics `json:"statistics"`
}

type Statistics struct {
	ServerThreadCount *int32       `json:"server_thread_count"`
	RpcsByMessageType *[]RPCByType `json:"rpcs_by_message_type"`
	RpcsByUser        *[]RPCByUser `json:"rpcs_by_user"`
}

type RPCByType struct {
	MessageType string `json:"message_type"`
	Count       int64  `json:"count"`
	TotalTime   int64  `json:"total_time"`
}

type RPCByUser struct {
	User      string `json:"user"`
	UserId    int32  `json:"user_id"`
	Count     int64  `json:"count"`
	TotalTime int64  `json:"total_time"`
}
