package nodelist

import (
	"fmt"
	"strconv"
	"strings"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// NodeList is a Slurm nodelist expression such as "worker-[0-3,7],worker-cpu-0". It is the
// form node lists take in slurm.conf, topology.yaml and the output of Slurm commands.
//
// Each comma-separated entry names either a single node, or literal text with up to two bracketed
// sets of numbers or ranges, each followed by more literal text: "rack[01-04]-gpu[0-7]". Numbers
// keep the zero padding of the range start, as Slurm does.
type NodeList string

// maxBracketSets is how many bracketed sets one entry may carry.
const maxBracketSets = 2

// NewNodeList collapses node names into the canonical nodelist expression. Names are deduplicated,
// grouped by prefix into ranges ordered by prefix, and names without a numeric suffix follow them,
// so two equal sets always render to the same expression.
func NewNodeList(nodeNames []string) NodeList {
	return NodeList(slurmpattern.Merge(nodeNames))
}

// Normalize rewrites the expression into the canonical form NewNodeList produces: the same nodes,
// deduplicated, with ranges collapsed and entries ordered as NewNodeList orders them. It fails on
// an expression that does not parse.
func Normalize(expression string) (NodeList, error) {
	nodelist := NodeList(expression)
	nodeNames, err := nodelist.NodeNames()
	if err != nil {
		return "", fmt.Errorf("normalize node list: %w", err)
	}
	return NewNodeList(nodeNames), nil
}

// NodeNames expands the expression back into node names, in the order the expression lists them:
// entry by entry, and within an entry with the first bracketed set varying slowest. An empty list
// expands to nil.
func (l NodeList) NodeNames() ([]string, error) {
	entries, err := splitEntries(string(l))
	if err != nil {
		return nil, fmt.Errorf("parse node list %q: %w", string(l), err)
	}

	var names []string
	for _, entry := range entries {
		expanded, err := expandEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("parse node list %q: %w", string(l), err)
		}
		names = append(names, expanded...)
	}
	return names, nil
}

// splitEntries splits on the commas outside brackets.
func splitEntries(list string) ([]string, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}

	var entries []string
	var current strings.Builder
	depth := 0
	flush := func() error {
		entry := strings.TrimSpace(current.String())
		if entry == "" {
			return fmt.Errorf("find empty entry")
		}
		entries = append(entries, entry)
		current.Reset()
		return nil
	}

	for _, r := range list {
		switch r {
		case '[':
			depth++
			if depth > 1 {
				return nil, fmt.Errorf("find nested '['")
			}
		case ']':
			if depth == 0 {
				return nil, fmt.Errorf("find ']' without a matching '['")
			}
			depth--
		case ',':
			if depth == 0 {
				if err := flush(); err != nil {
					return nil, err
				}
				continue
			}
		}
		current.WriteRune(r)
	}
	if depth != 0 {
		return nil, fmt.Errorf("find '[' without a matching ']'")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return entries, nil
}

// expandEntry expands one entry of literal text and bracketed sets into node names. Brackets are
// already known to be balanced and not nested.
func expandEntry(entry string) ([]string, error) {
	names := []string{""}
	rest := entry
	sets := 0
	for {
		open := strings.IndexByte(rest, '[')
		if open == -1 {
			break
		}
		sets++
		if sets > maxBracketSets {
			return nil, fmt.Errorf("parse entry %q: expected at most %d bracketed sets", entry, maxBracketSets)
		}
		closing := strings.IndexByte(rest, ']')
		numbers, err := expandSet(rest[open+1 : closing])
		if err != nil {
			return nil, fmt.Errorf("parse entry %q: %w", entry, err)
		}
		names = appendCrossProduct(names, rest[:open], numbers)
		rest = rest[closing+1:]
	}
	return appendCrossProduct(names, rest, []string{""}), nil
}

// appendCrossProduct extends every name with the literal and then with each number in turn, so
// earlier sets vary slower than later ones.
func appendCrossProduct(names []string, literal string, numbers []string) []string {
	extended := make([]string, 0, len(names)*len(numbers))
	for _, name := range names {
		for _, number := range numbers {
			extended = append(extended, name+literal+number)
		}
	}
	return extended
}

// expandSet expands the inside of one pair of brackets into formatted numbers, in the order the
// set lists them.
func expandSet(spec string) ([]string, error) {
	if spec == "" {
		return nil, fmt.Errorf("find empty brackets")
	}
	var numbers []string
	for _, part := range strings.Split(spec, ",") {
		start, end, width, err := parseRange(part)
		if err != nil {
			return nil, err
		}
		for n := start; n <= end; n++ {
			numbers = append(numbers, fmt.Sprintf("%0*d", width, n))
		}
	}
	return numbers, nil
}

// parseRange parses "7" or "3-12". The width is the number of digits of the first bound, which
// is how Slurm carries zero padding through a range.
func parseRange(part string) (start, end, width int, err error) {
	first, last, isRange := strings.Cut(part, "-")
	start, err = parseBound(first)
	if err != nil {
		return 0, 0, 0, err
	}
	end = start
	if isRange {
		if end, err = parseBound(last); err != nil {
			return 0, 0, 0, err
		}
		if end < start {
			return 0, 0, 0, fmt.Errorf("parse range %q: end precedes start", part)
		}
	}
	return start, end, len(first), nil
}

func parseBound(bound string) (int, error) {
	if bound == "" {
		return 0, fmt.Errorf("parse empty range bound")
	}
	for _, r := range bound {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("parse range bound %q: expected digits", bound)
		}
	}
	n, err := strconv.Atoi(bound)
	if err != nil {
		return 0, fmt.Errorf("parse range bound %q: %w", bound, err)
	}
	return n, nil
}
