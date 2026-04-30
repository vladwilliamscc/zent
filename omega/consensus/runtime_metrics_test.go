package consensus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"omega/runtimemetrics"
)

func TestCommitteeParticipationSuccessOnSigGivenTransition(t *testing.T) {
	t.Run("counts_minus1_to_other", func(t *testing.T) {
		runtimemetrics.ResetForTest()
		self := &Syncer{sigGiven: -1}

		self.commitSignature(3)

		if got := runtimemetrics.Read().CommitteeParticipationSuccessTotal; got != 1 {
			t.Fatalf("participation counter = %d, want 1", got)
		}
		if self.sigGiven != 3 {
			t.Fatalf("sigGiven = %d, want 3", self.sigGiven)
		}
	})

	t.Run("does_not_count_other_to_other", func(t *testing.T) {
		runtimemetrics.ResetForTest()
		self := &Syncer{sigGiven: 3}

		self.commitSignature(5)

		if got := runtimemetrics.Read().CommitteeParticipationSuccessTotal; got != 0 {
			t.Fatalf("participation counter = %d, want 0", got)
		}
		if self.sigGiven != 5 {
			t.Fatalf("sigGiven = %d, want 5", self.sigGiven)
		}
	})

	t.Run("does_not_count_minus1_to_minus1", func(t *testing.T) {
		runtimemetrics.ResetForTest()
		self := &Syncer{sigGiven: -1}

		self.commitSignature(-1)

		if got := runtimemetrics.Read().CommitteeParticipationSuccessTotal; got != 0 {
			t.Fatalf("participation counter = %d, want 0", got)
		}
		if self.sigGiven != -1 {
			t.Fatalf("sigGiven = %d, want -1", self.sigGiven)
		}
	})
}

func TestCommitteeDialFailureIncrementsWhenPeerNotConnected(t *testing.T) {
	file := parseConsensusRuntimeMetricFile(t, "syncer.go")
	if got := consensusFunctionConnectedCallCount(file, "repeater"); got != 1 {
		t.Fatalf("repeater Connected call count = %d, want exactly 1", got)
	}
	if !consensusFunctionContainsSelectorCall(file, "repeater", "runtimemetrics", "IncCommitteeDialFailure") {
		t.Fatalf("repeater missing runtimemetrics.IncCommitteeDialFailure call")
	}
}

func parseConsensusRuntimeMetricFile(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func consensusFunctionContainsSelectorCall(file *ast.File, fnName, pkgName, callName string) bool {
	fn := consensusFunctionDecl(file, fnName)
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != callName {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkgName {
			found = true
			return false
		}
		return true
	})
	return found
}

func consensusFunctionConnectedCallCount(file *ast.File, fnName string) int {
	fn := consensusFunctionDecl(file, fnName)
	if fn == nil || fn.Body == nil {
		return 0
	}
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Connected" {
			count++
		}
		return true
	})
	return count
}

func consensusFunctionDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}
