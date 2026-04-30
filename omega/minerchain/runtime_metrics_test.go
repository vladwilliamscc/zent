package minerchain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"btcd/blockchain/chainutil"
	"btcd/wire"
	"omega/runtimemetrics"
)

func TestCheckBlockContextIPMinerGapIncrementsCounter(t *testing.T) {
	runtimemetrics.ResetForTest()

	prevHeader := testCollateralMinerHeader(100)
	prevHeader.Connection = []byte("127.0.0.1:8333")
	prevNode := &chainutil.BlockNode{Data: &blockchainNodeData{block: prevHeader}}
	block := wire.NewMinerBlock(&wire.MingingRightBlock{
		Connection: []byte("127.0.0.1:8333"),
	})

	err := (&MinerChain{}).checkBlockContext(block, prevNode, 0)
	if err == nil {
		t.Fatalf("expected IP MinerGap rejection")
	}
	if got := runtimemetrics.Read().IPMinerGapRejectTotal; got != 1 {
		t.Fatalf("IPMinerGapRejectTotal = %d, want 1", got)
	}
}

func TestCheckBlockContextAddrMinerGapIncrementsCounter(t *testing.T) {
	runtimemetrics.ResetForTest()

	prevHeader := testCollateralMinerHeader(100)
	prevHeader.Miner[0] = 7
	prevNode := &chainutil.BlockNode{Data: &blockchainNodeData{block: prevHeader}}
	header := &wire.MingingRightBlock{}
	header.Miner[0] = 7
	block := wire.NewMinerBlock(header)

	err := (&MinerChain{}).checkBlockContext(block, prevNode, 0)
	if err == nil {
		t.Fatalf("expected address MinerGap rejection")
	}
	if got := runtimemetrics.Read().AddrMinerGapRejectTotal; got != 1 {
		t.Fatalf("AddrMinerGapRejectTotal = %d, want 1", got)
	}
}

func TestTemplateBuildSuccessIncrementsCounter(t *testing.T) {
	file := parseRuntimeMetricFile(t, "mining.go")
	if !runtimeMetricFunctionContainsSelectorCall(file, "generateBlocks", "runtimemetrics", "IncTemplateBuildSuccess") {
		t.Fatalf("generateBlocks missing runtimemetrics.IncTemplateBuildSuccess call")
	}
}

func TestTemplateBuildFailureIncrementsCounter(t *testing.T) {
	file := parseRuntimeMetricFile(t, "mining.go")
	if !runtimeMetricFunctionContainsSelectorCall(file, "generateBlocks", "runtimemetrics", "IncTemplateBuildFailure") {
		t.Fatalf("generateBlocks missing runtimemetrics.IncTemplateBuildFailure call")
	}
}

func TestMinerNonceTrialsAccumulatesAcrossTicks(t *testing.T) {
	file := parseRuntimeMetricFile(t, "mining.go")
	if !runtimeMetricFunctionContainsSelectorCall(file, "solveBlock", "runtimemetrics", "IncMinerNonceTrials") {
		t.Fatalf("solveBlock missing runtimemetrics.IncMinerNonceTrials call")
	}
	if !runtimeMetricFunctionContainsInc(file, "solveBlock", "nonceTrials") {
		t.Fatalf("solveBlock missing nonceTrials increment")
	}
}

func TestMinerNonceTrialsFlushedOnEarlyReturn(t *testing.T) {
	file := parseRuntimeMetricFile(t, "mining.go")
	if got := runtimeMetricFunctionIdentCallCount(file, "solveBlock", "flushNonceTrials"); got < 6 {
		t.Fatalf("solveBlock flushNonceTrials call count = %d, want at least 6 return/stale/solved paths", got)
	}
}

func parseRuntimeMetricFile(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func runtimeMetricFunctionContainsSelectorCall(file *ast.File, fnName, pkgName, callName string) bool {
	return runtimeMetricFunctionSelectorCallCount(file, fnName, pkgName, callName) > 0
}

func runtimeMetricFunctionSelectorCallCount(file *ast.File, fnName, pkgName, callName string) int {
	fn := runtimeMetricFunctionDecl(file, fnName)
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
		if !ok || sel.Sel.Name != callName {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkgName {
			count++
		}
		return true
	})
	return count
}

func runtimeMetricFunctionIdentCallCount(file *ast.File, fnName, callName string) int {
	fn := runtimeMetricFunctionDecl(file, fnName)
	if fn == nil || fn.Body == nil {
		return 0
	}
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == callName {
			count++
		}
		return true
	})
	return count
}

func runtimeMetricFunctionContainsInc(file *ast.File, fnName, identName string) bool {
	fn := runtimeMetricFunctionDecl(file, fnName)
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found {
			return false
		}
		inc, ok := node.(*ast.IncDecStmt)
		if !ok || inc.Tok.String() != "++" {
			return true
		}
		if ident, ok := inc.X.(*ast.Ident); ok && ident.Name == identName {
			found = true
			return false
		}
		return true
	})
	return found
}

func runtimeMetricFunctionDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}
