package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// declaredOnce refuses generated code that declares one top-level name twice, quoting both lines.
// `go vet` would refuse it too, but only after generation, naming a line in a file nobody keeps;
// this names what clashed, while the generator still knows which contract it came from.
func declaredOnce(code string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "client.gen.go", code, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("oapi-codegen wrote code that does not parse: %w", err)
	}
	lines := strings.Split(code, "\n")
	first := map[string]int{}
	var clashes []string
	note := func(name *ast.Ident) {
		line := fset.Position(name.Pos()).Line
		if earlier, seen := first[name.Name]; seen {
			clashes = append(clashes, fmt.Sprintf("%s, at line %d (%s) and line %d (%s)",
				name.Name, earlier, strings.TrimSpace(lines[earlier-1]), line, strings.TrimSpace(lines[line-1])))
			return
		}
		first[name.Name] = line
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				note(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					note(s.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							note(n)
						}
					}
				}
			}
		}
	}
	if len(clashes) > 0 {
		return fmt.Errorf("the generated client declares a name twice: %s", strings.Join(clashes, "; "))
	}
	return nil
}
