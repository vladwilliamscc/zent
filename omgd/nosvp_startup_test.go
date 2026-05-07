package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func nosvpReadOmgdSource(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(here), "omgd.go"))
	if err != nil {
		t.Fatalf("read omgd.go: %v", err)
	}
	return string(src)
}

func nosvpExtractFuncBody(t *testing.T, src, name string) string {
	t.Helper()

	header := "func " + name + "("
	headerIdx := strings.Index(src, header)
	if headerIdx < 0 {
		t.Fatalf("function %q not found", name)
	}
	open := strings.Index(src[headerIdx:], "{")
	if open < 0 {
		t.Fatalf("function %q has no opening brace", name)
	}
	depth := 0
	for i := headerIdx + open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[headerIdx : i+1]
			}
		}
	}
	t.Fatalf("unterminated function body for %q", name)
	return ""
}

// TestRunserverHasNoWaitGroupAdd locks in the wait-group accounting move:
// runserver no longer registers itself in the global WaitGroup; the launch
// loop owns that registration.
func TestRunserverHasNoWaitGroupAdd(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	body := nosvpExtractFuncBody(t, src, "runserver")
	if strings.Contains(body, "wg.Add(") {
		t.Fatalf("runserver still contains wg.Add(...): body=%q", body)
	}
}

// TestLaunchLoopRegistersWaitGroupBeforeRunserver locks in the launch-loop
// shape required by PR-16: p.running, then wg.Add(1), then go runserver(p)
// in that order, before the launch loop's child-RPC dispatch.
func TestLaunchLoopRegistersWaitGroupBeforeRunserver(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	pattern := regexp.MustCompile(`(?s)for i, p := range protocols \{\s*p\.running = true\s*wg\.Add\(1\)\s*go runserver\(p\)`)
	if !pattern.MatchString(src) {
		t.Fatalf("launch loop does not match the required p.running -> wg.Add(1) -> go runserver(p) shape")
	}
}

// TestCleanupOwnsWaitGroupDone preserves cleanup as the wg.Done owner. PR-16
// must not move wg.Done into runserver, the launch loop, or a defer.
func TestCleanupOwnsWaitGroupDone(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	cleanupBody := nosvpExtractFuncBody(t, src, "cleanup")
	if !strings.Contains(cleanupBody, "wg.Done()") {
		t.Fatalf("cleanup must still call wg.Done(); body=%q", cleanupBody)
	}
	runserverBody := nosvpExtractFuncBody(t, src, "runserver")
	if strings.Contains(runserverBody, "wg.Done(") {
		t.Fatalf("wg.Done must not appear in runserver")
	}
}

// TestSVPChildLoopGated verifies that the child protocol creation loop is
// wrapped in shouldStartSVPChildren(tcfg) and that --nosvp emits the public
// info log line.
func TestSVPChildLoopGated(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	if !strings.Contains(src, "if shouldStartSVPChildren(tcfg) {") {
		t.Fatalf("child loop is not gated by shouldStartSVPChildren(tcfg)")
	}
	if !strings.Contains(src, `chainmap.AllChains[Server.chainParams.MainChainID].ChainMap`) {
		t.Fatalf("child loop body shape changed unexpectedly")
	}
	if !strings.Contains(src, `btcdLog.Infof("SVP child-chain startup disabled by --nosvp")`) {
		t.Fatalf("--nosvp info log line missing or rewritten")
	}
}

// TestChainmapLoadStillReached confirms that chainmap.LoadChainMap is still
// called inside prepareServer; --nosvp gates child runtime startup, not
// chainmap metadata loading.
func TestChainmapLoadStillReached(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	prepareBody := nosvpExtractFuncBody(t, src, "prepareServer")
	if !strings.Contains(prepareBody, "chainmap.LoadChainMap(db, tcfg.TestNet, cid)") {
		t.Fatalf("prepareServer must still call chainmap.LoadChainMap(db, tcfg.TestNet, cid)")
	}
}

// TestAutoParentAddPreserved confirms that the auto-parent-add branch above
// the child loop is still present and is not gated by --nosvp.
func TestAutoParentAddPreserved(t *testing.T) {
	src := nosvpReadOmgdSource(t)
	autoIdx := strings.Index(src, "// terminate to cause reboot with new map")
	if autoIdx < 0 {
		t.Fatalf("auto-parent-add branch missing")
	}
	gateIdx := strings.Index(src, "if shouldStartSVPChildren(tcfg) {")
	if gateIdx < 0 {
		t.Fatalf("--nosvp gate missing")
	}
	if autoIdx >= gateIdx {
		t.Fatalf("auto-parent-add branch must precede the --nosvp gate (auto=%d gate=%d)", autoIdx, gateIdx)
	}
}
