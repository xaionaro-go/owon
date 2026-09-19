package owonmodel_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSemanticPackageEffects enforces selected pure/effectful ownership boundaries in authored source.
//
// Example: session admission or clock acquisition added to the model fails this regression.
func TestSemanticPackageEffects(t *testing.T) {
	for _, name := range []string{"owonmodel", "owonscpi", "owoncontrol", "owonusb", "owonrpc/owonserver", "owonrpc/owonclient"} {
		files, err := filepath.Glob(filepath.Join("..", name, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, files, name)
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, err)
			assertSemanticImports(t, name, path, file)
			assertSemanticEffects(t, name, path, file)
		}
	}
}

// assertSemanticImports checks ownership dependencies independently of test fixture imports.
//
// Example: production RPC must not import the local controller or physical connection implementation.
func assertSemanticImports(
	t *testing.T,
	name string,
	path string,
	file *ast.File,
) {
	t.Helper()
	for _, imp := range file.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)
		switch name {
		case "owonmodel":
			assert.False(t, strings.HasPrefix(value, "github.com/xaionaro-go/owon/pkg/"), "%s imports %s", path, value)
			assert.NotContains(t, []string{"context", "os", "io", "net", "sync"}, value, path)
		case "owonscpi":
			assert.NotContains(t, []string{"context", "os", "io", "net", "sync"}, value, path)
			assertExcludedOwners(t, path, value, "owoncontrol", "owonsession", "owonusb", "owonrpc")
		case "owonusb":
			assertExcludedOwners(t, path, value, "owoncontrol", "owonrpc")
		case "owonrpc/owonserver", "owonrpc/owonclient":
			assertExcludedOwners(t, path, value, "owoncontrol", "owonsession", "owonusb")
		}
	}
}

// assertExcludedOwners rejects specified sibling dependencies at a production boundary.
//
// Example: importing a server implementation from the physical backend is an ownership inversion.
func assertExcludedOwners(
	t *testing.T,
	path string,
	dependency string,
	owners ...string,
) {
	t.Helper()
	for _, owner := range owners {
		assert.False(t, strings.Contains(dependency, "/pkg/"+owner), "%s imports %s", path, dependency)
	}
}

// assertSemanticEffects checks executable expressions rather than banning legitimate time.Time observations.
//
// Example: aliases of the time package still cannot acquire a clock in model or dialect code.
func assertSemanticEffects(
	t *testing.T,
	name string,
	path string,
	file *ast.File,
) {
	t.Helper()
	clockName := "time"
	for _, imp := range file.Imports {
		if imp.Path.Value == `"time"` && imp.Name != nil {
			clockName = imp.Name.Name
		}
	}
	ast.Inspect(file,
		// inspectEffect checks each expression without executing code or walking test fixtures.
		//
		// Example: an SCPI literal in a controller fails even without a direct protocol-package import.
		func(node ast.Node) bool {
			if name == "owonmodel" || name == "owonscpi" {
				assert.False(t, acquiresClock(node, clockName), path)
			}
			if name == "owonmodel" || name == "owoncontrol" {
				assert.False(t, containsDialectCommand(node), path)
			}
			return true
		})
}

// acquiresClock recognizes time-package calls that read or schedule against a clock.
//
// Example: time.Now is an effect while a time.Time field is ordinary domain data.
func acquiresClock(
	node ast.Node,
	clockName string,
) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	if !ok || owner.Name != clockName {
		return false
	}
	switch selector.Sel.Name {
	case "Now", "Since", "Until", "Sleep", "After", "AfterFunc", "NewTimer", "NewTicker", "Tick":
		return true
	default:
		return false
	}
}

// containsDialectCommand recognizes HDS subsystem command literals at a forbidden boundary.
//
// Example: both :RUN and *IDN? belong to the dialect rather than the model or controller.
func containsDialectCommand(node ast.Node) bool {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return false
	}
	return value == "*IDN?" || strings.HasPrefix(value, ":") && len(value) > 1 && value[1] >= 'A' && value[1] <= 'Z'
}
