package sharedsteps

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// The budget names are the keys of the feature file table and the row names of the logged table.
const (
	stressBudgetAccepted      = "all jobs accepted"
	stressBudgetCompleted     = "all jobs completed"
	stressBudgetStartupP99    = "startup p99"
	stressBudgetStepsP99      = "steps p99"
	stressBudgetTeardownP99   = "teardown p99"
	stressBudgetSqueueMean    = "squeue mean"
	stressBudgetSinfoMean     = "sinfo mean"
	stressBudgetSqueueMax     = "squeue max"
	stressBudgetSinfoMax      = "sinfo max"
	stressBudgetSdiagMax      = "sdiag max"
	stressBudgetScheduleCycle = "main scheduler cycle mean"
	stressBudgetBackfillCycle = "backfill scheduler cycle mean"
	stressBudgetServerThreads = "server threads at the end"
)

const (
	// stressBudgetNearRatio is the share of a limit from which a passing budget is flagged in the table.
	stressBudgetNearRatio = 0.8
	// stressBudgetNotMeasured stands for an observed value the scenario could not obtain.
	stressBudgetNotMeasured = "-"
)

// stressBudgetRow is one budget of the load as logged and saved in summary.json.
type stressBudgetRow struct {
	Name     string `json:"name"`
	Observed string `json:"observed"`
	Limit    string `json:"limit"`
	// Used is the observed value as a share of the limit; it is 0 when the value was not measured.
	Used   float64 `json:"used"`
	Failed bool    `json:"failed"`
}

func newStressBudgetRow(name, observedText, limitText string, observed, limit int64) stressBudgetRow {
	row := stressBudgetRow{Name: name, Observed: observedText, Limit: limitText, Failed: observed > limit}
	if limit > 0 {
		row.Used = float64(observed) / float64(limit)
	}
	return row
}

func stressDurationBudget(name string, observed, limit time.Duration) stressBudgetRow {
	return newStressBudgetRow(name, observed.Round(time.Millisecond).String(), limit.String(), int64(observed), int64(limit))
}

func stressCountBudget(name string, observed, limit int64) stressBudgetRow {
	return newStressBudgetRow(name, strconv.FormatInt(observed, 10), strconv.FormatInt(limit, 10), observed, limit)
}

// notMeasured keeps the name and the limit of a row whose observed value is not valid.
func (r stressBudgetRow) notMeasured() stressBudgetRow {
	return stressBudgetRow{Name: r.Name, Observed: stressBudgetNotMeasured, Limit: r.Limit}
}

func stressSeconds(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func stressMillis(millis float64) time.Duration {
	return time.Duration(millis * float64(time.Millisecond))
}

func stressBudgetsNotMeasured(rows []stressBudgetRow) []stressBudgetRow {
	cleared := make([]stressBudgetRow, 0, len(rows))
	for _, row := range rows {
		cleared = append(cleared, row.notMeasured())
	}
	return cleared
}

// stressBudgetReport gathers the budget rows and the problems of every measurement of the load,
// so that one failed measurement or budget does not hide the others.
type stressBudgetReport struct {
	rows     []stressBudgetRow
	problems []string
}

// problem records a failure that is not a budget of its own, such as a measurement that could not be taken.
func (r *stressBudgetReport) problem(err error) {
	if err != nil {
		r.problems = append(r.problems, err.Error())
	}
}

// add records the rows evaluated from one measurement and the budgets they exceeded (budgetErr).
// Rows of a measurement that was not taken keep no observed values and no verdict.
func (r *stressBudgetReport) add(rows []stressBudgetRow, measured bool, budgetErr error) {
	if !measured {
		r.rows = append(r.rows, stressBudgetsNotMeasured(rows)...)
		return
	}
	r.rows = append(r.rows, rows...)
	r.problem(budgetErr)
}

func formatStressBudgets(rows []stressBudgetRow) string {
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprint(writer, "  BUDGET\tOBSERVED\tLIMIT\tUSED\n")
	for _, row := range rows {
		used := stressBudgetNotMeasured
		if row.Observed != stressBudgetNotMeasured {
			used = fmt.Sprintf("%3.0f%%", row.Used*100)
			switch {
			case row.Failed:
				used += "  FAIL"
			case row.Used >= stressBudgetNearRatio:
				used += "  near"
			}
		}
		fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\n", row.Name, row.Observed, row.Limit, used)
	}
	_ = writer.Flush()
	return strings.TrimRight(table.String(), "\n")
}
