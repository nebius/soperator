package slurmproxy

const (
	DefaultListenAddress = ":6821"
	DefaultAllowedUsers  = "root,soperatorchecks"
	DefaultClientUser    = "root"
	DefaultReason        = "soperator rolling update"

	EndpointHealthz     = "/healthz"
	EndpointRebootNodes = "/v1/nodes/reboot"
)

type RebootNodesRequest struct {
	Nodes  []string `json:"nodes"`
	Reason string   `json:"reason,omitempty"`
}

type RebootNodesResponse struct {
	Nodes  []string `json:"nodes"`
	Output string   `json:"output,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
