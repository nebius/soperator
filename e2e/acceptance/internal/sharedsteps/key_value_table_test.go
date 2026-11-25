package sharedsteps

import (
	"testing"
	"time"

	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v34"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyValueTable(t *testing.T) {
	values, err := parseKeyValueTable(newTable([]string{"squeue mean", "1s"}, []string{" server threads at the end ", "10"}))
	require.NoError(t, err)
	assert.Equal(t, time.Second, values.duration("squeue mean"))
	assert.Equal(t, int64(10), values.count("server threads at the end"))
	require.NoError(t, values.done())

	values, err = parseKeyValueTable(newTable([]string{"squeue p95", "soon"}, []string{"threads", "-1"}, []string{"sinfo p95", "2s"}, []string{"sdiag p50", "3s"}))
	require.NoError(t, err)
	values.duration("squeue p95")
	values.count("threads")
	values.duration("sdiag max")
	err = values.done()
	require.Error(t, err)
	for _, expected := range []string{
		`"squeue p95": "soon" is not a positive duration`,
		`"threads": "-1" is not a positive count`,
		`key "sdiag max" is missing`,
		`2 keys are not known to this step (sdiag p50, sinfo p95)`,
	} {
		assert.Contains(t, err.Error(), expected)
	}

	_, err = parseKeyValueTable(nil)
	require.ErrorContains(t, err, "needs a table")
	_, err = parseKeyValueTable(newTable([]string{"only name"}))
	require.ErrorContains(t, err, "row 1 has 1 cells")
	_, err = parseKeyValueTable(newTable([]string{"squeue p95", ""}))
	require.ErrorContains(t, err, "row 1 lacks a key or a value")
	_, err = parseKeyValueTable(newTable([]string{"squeue p95", "1s"}, []string{"squeue p95", "2s"}))
	require.ErrorContains(t, err, `key "squeue p95" is listed twice`)
}

// newTable builds a Gherkin table from its rows for step tests.
func newTable(rows ...[]string) *godog.Table {
	table := &godog.Table{}
	for _, values := range rows {
		row := &messages.PickleTableRow{}
		for _, value := range values {
			row.Cells = append(row.Cells, &messages.PickleTableCell{Value: value})
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}
