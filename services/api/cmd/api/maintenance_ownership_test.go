package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// apiProcessSweeps are the only background loops an API replica may start. The pricing
// activator refreshes the replica's own in-memory copy; the shared-asset sweep deletes
// objects with application object-store keys the worker does not hold.
var apiProcessSweeps = map[string]bool{"RunActivator": true, "MaintainAssets": true}

// TestAPIProcessStartsNoMaintenanceLoop keeps every Maintain loop in the worker, so a sweep
// runs once however many API replicas serve HTTP.
func TestAPIProcessStartsNoMaintenanceLoop(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var run *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "run" {
			run = fn
		}
	}
	if run == nil {
		t.Fatal("run not found")
	}
	var workerBlock *ast.BlockStmt
	for _, stmt := range run.Body.List {
		if cond, ok := stmt.(*ast.IfStmt); ok && strings.Contains(exprText(cond.Cond), `"worker"`) && strings.Contains(exprText(cond.Cond), "command") && cond.Else == nil {
			if last := cond.Body.List; len(last) > 0 {
				if _, returns := last[len(last)-1].(*ast.ReturnStmt); returns {
					workerBlock = cond.Body
				}
			}
		}
	}
	if workerBlock == nil {
		t.Fatal("the worker branch of run was not found")
	}
	isSweep := func(name string) bool {
		return strings.HasPrefix(name, "Maintain") || strings.HasPrefix(name, "RunMaintenance") || name == "RunActivator"
	}
	worker, api := 0, 0
	ast.Inspect(run, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isSweep(sel.Sel.Name) {
			return true
		}
		if n.Pos() >= workerBlock.Pos() && n.End() <= workerBlock.End() {
			worker++
			return true
		}
		api++
		if !apiProcessSweeps[sel.Sel.Name] {
			t.Errorf("%s: the API process starts %s; the worker owns every Maintain loop", fset.Position(n.Pos()), sel.Sel.Name)
		}
		return true
	})
	if worker == 0 {
		t.Error("the worker starts no maintenance loop; the sweeps have no owner")
	}
	if api != len(apiProcessSweeps) {
		t.Errorf("the API process starts %d sweeps, want exactly the %d documented ones", api, len(apiProcessSweeps))
	}
}

func exprText(e ast.Expr) string {
	var sb strings.Builder
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			sb.WriteString(v.Name + " ")
		case *ast.BasicLit:
			sb.WriteString(v.Value + " ")
		}
		return true
	})
	return sb.String()
}

// TestWorkerSweepsOwnTheMovedLoops pins the loops that moved out of the API process.
func TestWorkerSweepsOwnTheMovedLoops(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "worker_sweeps.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Maintain") {
			got[sel.Sel.Name] = true
		}
		return true
	})
	if !got["Maintain"] || !got["MaintainInvites"] {
		t.Fatalf("worker sweeps start %v, want Maintain and MaintainInvites", got)
	}
}
