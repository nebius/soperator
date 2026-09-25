package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindAccountingCluster(t *testing.T) {
	record, err := findAccountingCluster(
		"other|controller.other|6817|\nsoperator|controller.soperator|6817|\n",
		"soperator",
	)
	require.NoError(t, err)
	assert.Equal(t, accountingClusterRecord{
		Cluster:     "soperator",
		ControlHost: "controller.soperator",
		ControlPort: "6817",
	}, record)
}

func TestFindAccountingClusterRejectsIncompleteEndpoint(t *testing.T) {
	_, err := findAccountingCluster("soperator||6817|\n", "soperator")
	assert.ErrorContains(t, err, "incomplete control endpoint")
}

func TestAccountingAssociationExists(t *testing.T) {
	output := "soperator|other|bob|\nsoperator|e2e-research|bob|\n"
	assert.True(t, accountingAssociationExists(output, "soperator", "e2e-research", "bob"))
	assert.False(t, accountingAssociationExists(output, "soperator", "e2e-research", "alice"))
}

func TestFindAndValidateAccountingJobRecord(t *testing.T) {
	output := "42|e2e-accounting|bob|e2e-research|COMPLETED|0:0|00:00:02|2|1|2|billing=2,cpu=2,mem=1436G,node=1|2026-09-02T10:00:00|2026-09-02T10:00:02|\n"
	record, found := findAccountingJobRecord(output, "42")
	require.True(t, found)
	assert.NoError(t, validateAccountingJobRecord(record))
}

func TestValidateAccountingJobRecordReportsMismatches(t *testing.T) {
	record := accountingJobRecord{
		JobID:      "42",
		JobName:    "wrong",
		User:       "alice",
		Account:    "other",
		State:      "FAILED",
		ExitCode:   "1:0",
		Elapsed:    "",
		ElapsedRaw: "0",
		Nodes:      "2",
		CPUs:       "0",
		AllocTRES:  "billing=0,cpu=0",
		Start:      "Unknown",
		End:        "Unknown",
	}
	err := validateAccountingJobRecord(record)
	require.Error(t, err)
	for _, expected := range []string{
		`job name="wrong"`,
		`user="alice"`,
		`account="other"`,
		`state="FAILED"`,
		`exit code="1:0"`,
		`elapsed=""`,
		`elapsed raw="0"`,
		`nodes="2"`,
		`CPUs="0"`,
		`allocated CPU TRES="0"`,
		`allocated billing TRES="0"`,
		`start="Unknown"`,
		`end="Unknown"`,
	} {
		assert.ErrorContains(t, err, expected)
	}
}

func TestParseAccountingRowsTrimsFieldsAndTrailingDelimiter(t *testing.T) {
	assert.Equal(t, [][]string{{"soperator", "bob", "e2e-research"}},
		parseAccountingRows("  soperator | bob | e2e-research |  \n\n"))
}
