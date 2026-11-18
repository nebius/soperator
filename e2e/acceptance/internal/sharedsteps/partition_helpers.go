package sharedsteps

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// partitionNodesCommand lists every node of the partition with its socket and core counts and state;
// a node may repeat when it belongs to several partitions.
func partitionNodesCommand(partition string) string {
	return "sinfo --noheader --Node --partition=" + framework.ShellQuote(partition) + " --format='%N|%X|%Y|%T'"
}

// partitionNode is a node of a partition as sinfo reports it.
type partitionNode struct {
	Name  string
	Cores int
	State string
}

// parsePartitionNodes parses sinfo rows of the form name|sockets|cores-per-socket|state
// into nodes sorted by name; a node listed several times counts once.
func parsePartitionNodes(sinfoOutput string) ([]partitionNode, error) {
	byName := map[string]partitionNode{}
	for _, fields := range parsePipeRows(sinfoOutput) {
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected sinfo row %q", strings.Join(fields, "|"))
		}
		name := fields[0]
		sockets, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse sockets of node %s: %w", name, err)
		}
		perSocket, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("parse cores per socket of node %s: %w", name, err)
		}
		byName[name] = partitionNode{
			Name:  name,
			Cores: sockets * perSocket,
			State: fields[3],
		}
	}
	nodes := make([]partitionNode, 0, len(byName))
	for _, node := range byName {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}
