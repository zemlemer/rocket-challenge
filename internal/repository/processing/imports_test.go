package processing_test

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The processing adapter serves only the processor port; importing the HTTP
// layer would undo the package split (design D5 of split-repository-packages).
func TestDoesNotImportHandler(t *testing.T) {
	const forbidden = "rockets/internal/handler"
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
