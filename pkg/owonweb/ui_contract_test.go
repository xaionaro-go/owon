package owonweb

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"golang.org/x/net/html"
)

// TestEmbeddedStreamPolicy ties static presentation to authoritative Go policy.
//
// Example: the queue input starts at one because API zero is a default sentinel, not an editable queue size.
func TestEmbeddedStreamPolicy(t *testing.T) {
	t.Parallel()
	content, err := embeddedUI.ReadFile("ui/index.html")
	require.NoError(t, err)
	require.NoError(t, checkEmbeddedStreamPolicy(string(content)))
	mutated := strings.Replace(string(content), `id="stream-queue" type="number" min="1" max="1024"`, `id="stream-queue" type="number" min="1" max="1025"`, 1)
	require.NotEqual(t, string(content), mutated)
	require.ErrorContains(t, checkEmbeddedStreamPolicy(mutated), "stream-queue max")
}

// checkEmbeddedStreamPolicy compares numeric attributes without duplicating runtime normalization.
//
// Example: changing the shared maximum requires updating the HTML presentation too.
func checkEmbeddedStreamPolicy(content string) error {
	inputs, err := embeddedInputAttributes(content)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		ID        string
		Attribute string
		Expected  int64
	}{
		{ID: "stream-interval", Attribute: "min", Expected: owonrpc.MinimumSubscriptionInterval.Milliseconds()},
		{ID: "stream-interval", Attribute: "max", Expected: maxSubscribeInterval.Milliseconds()},
		{ID: "stream-interval", Attribute: "value", Expected: defaultWebSubscriptionInterval.Milliseconds()},
		{ID: "stream-queue", Attribute: "min", Expected: 1},
		{ID: "stream-queue", Attribute: "max", Expected: owonrpc.MaximumSubscriptionCapacity},
		{ID: "stream-queue", Attribute: "value", Expected: defaultWebSubscriptionCapacity},
	} {
		if value := inputs[field.ID][field.Attribute]; value != strconv.FormatInt(field.Expected, 10) {
			return fmt.Errorf("%s %s: got %q, want %d", field.ID, field.Attribute, value, field.Expected)
		}
	}
	return nil
}

// TestEmbeddedChannelCouplingChoices checks both actual optional selects, not the JS fake DOM.
//
// Example: Ground is reachable for channels while remaining absent from trigger coupling.
func TestEmbeddedChannelCouplingChoices(t *testing.T) {
	t.Parallel()
	content, err := embeddedUI.ReadFile("ui/index.html")
	require.NoError(t, err)
	forms, err := embeddedCouplingOptions(string(content))
	require.NoError(t, err)
	require.Equal(t, [][]string{{"", "COUPLING_DC", "COUPLING_AC", "COUPLING_GROUND"}, {"", "COUPLING_DC", "COUPLING_AC", "COUPLING_GROUND"}}, forms["/api/channel"])
	require.Equal(t, [][]string{{"", "COUPLING_DC", "COUPLING_AC"}}, forms["/api/trigger"])
	secondForm := strings.LastIndex(string(content), `<form data-api="/api/channel"`)
	selectOffset := secondForm + strings.Index(string(content)[secondForm:], `<select name="coupling"`)
	outside := string(content[:selectOffset]) + "</form>" + string(content[selectOffset:])
	misplaced, err := embeddedCouplingOptions(outside)
	require.NoError(t, err)
	require.Len(t, misplaced["/api/channel"], 1, "a select outside CH2 cannot satisfy its form contract")
}

// embeddedCouplingOptions extracts the real form-scoped option values from embedded markup.
//
// Example: an option in an unrelated form cannot satisfy the second channel's contract.
func embeddedCouplingOptions(content string) (map[string][][]string, error) {
	forms := make(map[string][][]string)
	tokens := html.NewTokenizer(strings.NewReader(content))
	form := ""
	coupling := false
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		token := tokens.Token()
		if kind == html.EndTagToken && token.Data == "form" {
			form = ""
		}
		if kind == html.EndTagToken {
			coupling = coupling && token.Data != "select"
			continue
		}
		if kind != html.StartTagToken {
			continue
		}
		attributes := htmlAttributes(token)
		switch {
		case token.Data == "form":
			form = attributes["data-api"]
		case token.Data == "select" && attributes["name"] == "coupling" && form != "":
			coupling = true
			forms[form] = append(forms[form], nil)
		case token.Data == "select":
			coupling = false
		case token.Data == "option" && coupling:
			index := len(forms[form]) - 1
			forms[form][index] = append(forms[form][index], attributes["value"])
		}
	}
	if tokens.Err() != io.EOF {
		return nil, tokens.Err()
	}
	return forms, nil
}

// htmlAttributes turns one token's declared attributes into a test-readable map.
//
// Example: numeric input min/max/value attributes remain presentation data.
func htmlAttributes(token html.Token) map[string]string {
	attributes := make(map[string]string)
	for _, attribute := range token.Attr {
		attributes[attribute.Key] = attribute.Val
	}
	return attributes
}

// embeddedInputAttributes reads input constraints from actual HTML and retains parse failures.
//
// Example: the stream interval contract can compare its presentation with Go policy.
func embeddedInputAttributes(content string) (map[string]map[string]string, error) {
	inputs := make(map[string]map[string]string)
	tokens := html.NewTokenizer(strings.NewReader(content))
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		token := tokens.Token()
		if kind != html.StartTagToken || token.Data != "input" {
			continue
		}
		attributes := htmlAttributes(token)
		inputs[attributes["id"]] = attributes
	}
	if tokens.Err() != io.EOF {
		return nil, tokens.Err()
	}
	return inputs, nil
}
