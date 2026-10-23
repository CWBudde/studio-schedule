package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestIDTypesListsEveryDomainID reads internal/domain/id.go and requires
// idTypes to contain every ID type declared there (`XID = ID[xKind]`), so
// a new ID type cannot miss its schema registration.
func TestIDTypesListsEveryDomainID(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "../domain/id.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	registered := map[string]bool{}
	for _, typ := range idTypes {
		registered[typ.Name()] = true
	}

	declared := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}

		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ts.Assign.IsValid() {
				continue
			}

			index, ok := ts.Type.(*ast.IndexExpr)
			if !ok || index.X.(*ast.Ident).Name != "ID" {
				continue
			}

			declared++
			kind := index.Index.(*ast.Ident).Name
			// reflect names an instantiation ID[<pkg path>.<kind>].
			want := "ID[github.com/cwbudde/studio-schedule/internal/domain." + kind + "]"
			if !registered[want] {
				t.Errorf("domain.%s is not in idTypes (looked for %s)", ts.Name.Name, want)
			}
		}
	}

	if declared != len(idTypes) || declared == 0 {
		t.Errorf("domain declares %d ID types, idTypes has %d", declared, len(idTypes))
	}
}
