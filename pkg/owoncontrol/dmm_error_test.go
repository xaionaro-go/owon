package owoncontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
)

// TestDMMConvergenceWriteCountGrammar keeps diagnostics grammatical at every
// useful completed-write count.
//
// Example: one completed CONFIGURE write uses the singular form.
func TestDMMConvergenceWriteCountGrammar(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Count int
		Want  string
	}{
		{Count: 0, Want: "no writes completed"},
		{Count: 1, Want: "1 write completed"},
		{Count: 2, Want: "2 writes completed"},
	} {
		err := (&ErrDMMConvergence{
			Phase:                                  DMMConvergencePhaseInitial,
			RequestedSelection:                     owonmodel.DMMFunctionSelection{Function: owonmodel.DMMFunctionDiode},
			SuccessfulTransportCompletedWriteCount: testCase.Count,
		}).Error()
		require.Contains(t, err, testCase.Want, testCase.Count)
	}
}
