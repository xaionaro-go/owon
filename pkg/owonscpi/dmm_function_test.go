package owonscpi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// TestDMMFunctionQueryUsesTheDocumentedReadbackForms verifies the pure target query selection.
//
// Example: resistance uses the generic query while voltage uses its function-specific query.
func TestDMMFunctionQueryUsesTheDocumentedReadbackForms(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Function owonmodel.DMMFunction
		Command  string
	}{
		{owonmodel.DMMFunctionVoltage, ":DMM:CONFIGURE:VOLTAGE?"},
		{owonmodel.DMMFunctionCurrent, ":DMM:CONFIGURE:CURRENT?"},
		{owonmodel.DMMFunctionResistance, ":DMM:CONFIGURE?"},
		{owonmodel.DMMFunctionCapacitance, ":DMM:CONFIGURE?"},
		{owonmodel.DMMFunctionDiode, ":DMM:CONFIGURE?"},
		{owonmodel.DMMFunctionContinuity, ":DMM:CONFIGURE?"},
	} {
		command, err := DMMFunctionQuery(testCase.Function)
		require.NoError(t, err)
		require.Equal(t, owonprotocol.ResponseModeASCII, command.ResponseMode)
		require.Equal(t, testCase.Command, command.Text)
	}
	_, err := DMMFunctionQuery(owonmodel.DMMFunctionUnspecified)
	var invalid *owonmodel.ErrInvalidRequest
	require.ErrorAs(t, err, &invalid)
}

// TestDMMRangeQueryAndParserUseOnlyDocumentedTypedTokens verifies range readback stays within the model enum.
//
// Example: a documented mV response becomes DMMRangeMV while an unverified device token is rejected.
func TestDMMRangeQueryAndParserUseOnlyDocumentedTypedTokens(t *testing.T) {
	t.Parallel()

	query := DMMRangeQuery()
	require.Equal(t, ":DMM:RANGE?", query.Text)
	require.Equal(t, owonprotocol.ResponseModeASCII, query.ResponseMode)
	for _, testCase := range []struct {
		Response string
		Want     owonmodel.DMMRange
	}{
		{Response: "ON", Want: owonmodel.DMMRangeOn},
		{Response: "mV", Want: owonmodel.DMMRangeMV},
		{Response: "V", Want: owonmodel.DMMRangeV},
		{Response: " v ", Want: owonmodel.DMMRangeV},
	} {
		got, err := ParseDMMRange([]byte(testCase.Response))
		require.NoError(t, err, testCase.Response)
		require.Equal(t, testCase.Want, got, testCase.Response)
	}
	for _, response := range []string{"", "10A", "unknown", "V extra"} {
		got, err := ParseDMMRange([]byte(response))
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, response)
		require.Zero(t, got, response)
	}
}

// TestCompileDMMKeepsFunctionCommandFirst verifies the explicit function-first compilation invariant.
//
// Example: CONFIGURE precedes REL, RANGE, and AUTO in the one compiled command sequence.
func TestCompileDMMKeepsFunctionCommandFirst(t *testing.T) {
	t.Parallel()
	diode, relative, rangeValue, autoRange := owonmodel.DMMFunctionDiode, true, owonmodel.DMMRangeV, true
	commands, err := CompileDMM(&owonmodel.DMMPatch{
		Function:  &diode,
		Relative:  &relative,
		Range:     &rangeValue,
		AutoRange: &autoRange,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		":DMM:CONFIGURE DIODE",
		":DMM:REL ON",
		":DMM:RANGE V",
		":DMM:AUTO ON",
	}, commandTexts(commands))
	for _, testCase := range []struct {
		Function    owonmodel.DMMFunction
		CurrentType owonmodel.DMMCurrentType
		HasCurrent  bool
		Command     string
	}{
		{Function: owonmodel.DMMFunctionVoltage, CurrentType: owonmodel.DMMCurrentTypeDC, HasCurrent: true, Command: ":DMM:CONFIGURE:VOLTAGE DC"},
		{Function: owonmodel.DMMFunctionCurrent, CurrentType: owonmodel.DMMCurrentTypeAC, HasCurrent: true, Command: ":DMM:CONFIGURE:CURRENT AC"},
		{Function: owonmodel.DMMFunctionResistance, Command: ":DMM:CONFIGURE RESISTANCE"},
		{Function: owonmodel.DMMFunctionCapacitance, Command: ":DMM:CONFIGURE CAPACITANCE"},
		{Function: owonmodel.DMMFunctionDiode, Command: ":DMM:CONFIGURE DIODE"},
		{Function: owonmodel.DMMFunctionContinuity, Command: ":DMM:CONFIGURE CONTINUITY"},
	} {
		functionValue := testCase.Function
		patch := &owonmodel.DMMPatch{Function: &functionValue}
		if testCase.HasCurrent {
			currentType := testCase.CurrentType
			patch.CurrentType = &currentType
		}
		commands, err := CompileDMM(patch)
		require.NoError(t, err, testCase.Command)
		require.Equal(t, []string{testCase.Command}, commandTexts(commands), testCase.Command)
	}
}

// TestParseDMMFunctionRecognizesEveryGenericFunction verifies the finite generic token set.
//
// Example: a capacitance reply is a valid observation even when the requested target is diode.
func TestParseDMMFunctionRecognizesEveryGenericFunction(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Requested owonmodel.DMMFunction
		Token     string
		Observed  owonmodel.DMMFunction
	}{
		{Requested: owonmodel.DMMFunctionResistance, Token: "RESistance", Observed: owonmodel.DMMFunctionResistance},
		{Requested: owonmodel.DMMFunctionCapacitance, Token: "CAPacitance", Observed: owonmodel.DMMFunctionCapacitance},
		{Requested: owonmodel.DMMFunctionDiode, Token: "DIODe", Observed: owonmodel.DMMFunctionDiode},
		{Requested: owonmodel.DMMFunctionContinuity, Token: "CONTinuity", Observed: owonmodel.DMMFunctionContinuity},
		{Requested: owonmodel.DMMFunctionVoltage, Token: "VOLTage", Observed: owonmodel.DMMFunctionVoltage},
		{Requested: owonmodel.DMMFunctionCurrent, Token: "CURRent", Observed: owonmodel.DMMFunctionCurrent},
	} {
		selection, err := ParseDMMFunction(testCase.Requested, []byte(testCase.Token))
		require.NoError(t, err, testCase.Token)
		require.Equal(t, testCase.Observed, selection.Function, testCase.Token)
		require.Nil(t, selection.CurrentType, testCase.Token)
	}
}

// TestParseDMMFunctionClassifiesObservedTokens verifies nonmatches and transient device errors.
//
// Example: a generic resistance token is a valid nonmatch for a diode target, while `error` retries.
func TestParseDMMFunctionClassifiesObservedTokens(t *testing.T) {
	t.Parallel()
	diode := owonmodel.DMMFunctionDiode
	selection, err := ParseDMMFunction(diode, []byte("RESistance\r\n"))
	require.NoError(t, err)
	require.Equal(t, owonmodel.DMMFunctionResistance, selection.Function)
	require.Nil(t, selection.CurrentType)
	_, err = ParseDMMFunction(diode, []byte("error\n"))
	var unavailable *ErrDMMFunctionUnavailable
	require.ErrorAs(t, err, &unavailable)
	require.Nil(t, errors.Unwrap(err))
	for _, response := range []string{"", "123", "UNKNOWN", "DIODE extra"} {
		_, err = ParseDMMFunction(diode, []byte(response))
		var malformed *ErrMalformedResponse
		require.ErrorAs(t, err, &malformed, response)
	}
	_, err = ParseDMMFunction(diode, []byte("ERROR"))
	var malformed *ErrMalformedResponse
	require.ErrorAs(t, err, &malformed)
}

// TestParseDMMFunctionKeepsVoltageCurrentRepliesAsQueryContext verifies AC/DC semantics.
//
// Example: a DC reply supplies the requested query context and observed type, not a fabricated global state.
func TestParseDMMFunctionKeepsVoltageCurrentRepliesAsQueryContext(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		Function    owonmodel.DMMFunction
		CurrentType owonmodel.DMMCurrentType
	}{
		{owonmodel.DMMFunctionVoltage, owonmodel.DMMCurrentTypeDC},
		{owonmodel.DMMFunctionCurrent, owonmodel.DMMCurrentTypeAC},
	} {
		selection, err := ParseDMMFunction(testCase.Function, []byte(map[owonmodel.DMMCurrentType]string{
			owonmodel.DMMCurrentTypeAC: "AC",
			owonmodel.DMMCurrentTypeDC: "DC",
		}[testCase.CurrentType]))
		require.NoError(t, err)
		require.Equal(t, testCase.Function, selection.Function)
		require.NotNil(t, selection.CurrentType)
		require.Equal(t, testCase.CurrentType, *selection.CurrentType)
	}
}

// TestParseDMMFunctionRejectsCurrentTypeRepliesForGenericFunctions keeps AC/DC
// observations within the function-specific query contract.
//
// Example: a generic diode query receiving AC is malformed rather than a
// fabricated diode/current selection.
func TestParseDMMFunctionRejectsCurrentTypeRepliesForGenericFunctions(t *testing.T) {
	t.Parallel()
	for _, function := range []owonmodel.DMMFunction{
		owonmodel.DMMFunctionResistance,
		owonmodel.DMMFunctionCapacitance,
		owonmodel.DMMFunctionDiode,
		owonmodel.DMMFunctionContinuity,
	} {
		for _, token := range []string{"AC", "DC"} {
			selection, err := ParseDMMFunction(function, []byte(token))
			var malformed *ErrMalformedResponse
			require.ErrorAs(t, err, &malformed, "function=%v token=%s", function, token)
			require.Nil(t, selection, "function=%v token=%s", function, token)
		}
	}
}

// commandTexts returns command text without exposing response framing in order assertions.
//
// Example: compile tests compare the exact CONFIGURE, REL, RANGE, and AUTO order.
func commandTexts(commands []owonprotocol.Command) []string {
	texts := make([]string, 0, len(commands))
	for _, command := range commands {
		texts = append(texts, command.Text)
	}
	return texts
}
