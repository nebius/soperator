package sharedsteps

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// keyValueTable hands out the values of a two-column Gherkin table by key, typed and
// once each, so that a key the feature file misspells, omits or adds by mistake fails the step.
type keyValueTable struct {
	values   map[string]string
	problems []string
}

func parseKeyValueTable(table *godog.Table) (*keyValueTable, error) {
	if table == nil || len(table.Rows) == 0 {
		return nil, errors.New("the step needs a table of keys and values")
	}
	values := make(map[string]string, len(table.Rows))
	for i, row := range table.Rows {
		if len(row.Cells) != 2 {
			return nil, fmt.Errorf("row %d has %d cells, expected a key and a value", i+1, len(row.Cells))
		}
		key, value := strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("row %d lacks a key or a value", i+1)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("key %q is listed twice", key)
		}
		values[key] = value
	}
	return &keyValueTable{values: values}, nil
}

// take removes and returns the value of key, recording a problem when it is missing.
func (t *keyValueTable) take(key string) (string, bool) {
	value, found := t.values[key]
	if !found {
		t.problems = append(t.problems, fmt.Sprintf("key %q is missing", key))
		return "", false
	}
	delete(t.values, key)
	return value, true
}

func (t *keyValueTable) duration(key string) time.Duration {
	value, found := t.take(key)
	if !found {
		return 0
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		t.problems = append(t.problems, fmt.Sprintf("%q: %q is not a positive duration", key, value))
	}
	return parsed
}

func (t *keyValueTable) count(key string) int64 {
	value, found := t.take(key)
	if !found {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		t.problems = append(t.problems, fmt.Sprintf("%q: %q is not a positive count", key, value))
	}
	return parsed
}

// done reports every key that was missing, malformed or not understood by the step.
func (t *keyValueTable) done() error {
	problems := t.problems
	if len(t.values) > 0 {
		unknown := make([]string, 0, len(t.values))
		for key := range t.values {
			unknown = append(unknown, key)
		}
		sort.Strings(unknown)
		problems = append(problems, fmt.Sprintf("%d keys are not known to this step (%s)", len(unknown), strings.Join(unknown, ", ")))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}
