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
	output := "42|e2e-accounting|bob|e2e-research|COMPLETED|0:0|00:00:01|2026-09-02T10:00:00|2026-09-02T10:00:01|\n"
	record, found := findAccountingJobRecord(output, "42")
	require.True(t, found)
	assert.NoError(t, validateAccountingJobRecord(record))
}

func TestValidateAccountingJobRecordReportsMismatches(t *testing.T) {
	record := accountingJobRecord{
		JobID:    "42",
		JobName:  "wrong",
		User:     "alice",
		Account:  "other",
		State:    "FAILED",
		ExitCode: "1:0",
		Elapsed:  "",
		Start:    "Unknown",
		End:      "Unknown",
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
		`start="Unknown"`,
		`end="Unknown"`,
	} {
		assert.ErrorContains(t, err, expected)
	}
}

func TestAccountingReportContainsRow(t *testing.T) {
	output := "other|bob|e2e-research|10|\nsoperator|bob|e2e-research|20|\n"
	assert.True(t, accountingReportContainsRow(output,
		[]string{"soperator", "bob", "e2e-research"}))
	assert.False(t, accountingReportContainsRow(output,
		[]string{"soperator", "alice", "e2e-research"}))
}

func TestParseAccountingRowsTrimsFieldsAndTrailingDelimiter(t *testing.T) {
	assert.Equal(t, [][]string{{"soperator", "bob", "e2e-research"}},
		parseAccountingRows("  soperator | bob | e2e-research |  \n\n"))
}
