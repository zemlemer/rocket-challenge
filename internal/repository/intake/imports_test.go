package intake_test

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The intake adapters serve only the handler ports; importing the processor
// would undo the package split (design D5 of split-repository-packages).
func TestDoesNotImportProcessor(t *testing.T) {
	const forbidden = "rockets/internal/processor"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == forbidden {
				t.Errorf("%s imports %s", name, forbidden)
			}
		}
	}
}
