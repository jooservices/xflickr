// Command gofuncstats prints one tab-separated line per function declared in
// the Go files it reads, one path per line on stdin:
//
//	file  name  lines  complexity  params
//
// Complexity counts like cyclop: 1 plus each if, for, range, non-default case,
// select case, && and ||. Lines span the func keyword to the closing brace.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		if err := report(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "gofuncstats: read paths:", err)
		os.Exit(2)
	}
}

func report(path string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("gofuncstats: parse %s: %w", path, err)
	}
	seen := map[string]int{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := funcName(fn)
		seen[name]++
		if seen[name] > 1 {
			name = fmt.Sprintf("%s#%d", name, seen[name])
		}
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		fmt.Printf("%s\t%s\t%d\t%d\t%d\n", path, name, lines, complexity(fn.Body), paramCount(fn.Type))
	}
	return nil
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return recvName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func recvName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

func complexity(body *ast.BlockStmt) int {
	if body == nil {
		return 1
	}
	score := 1
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			score++
		case *ast.CaseClause:
			if x.List != nil {
				score++
			}
		case *ast.CommClause:
			if x.Comm != nil {
				score++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				score++
			}
		}
		return true
	})
	return score
}

func paramCount(ft *ast.FuncType) int {
	count := 0
	for _, field := range ft.Params.List {
		count += max(1, len(field.Names))
	}
	return count
}
