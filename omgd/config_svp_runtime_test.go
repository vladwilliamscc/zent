package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"btcd/chaincfg"
)

func withTestConfigGlobals(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()

	oldHomeDir := defaultHomeDir
	oldConfigFile := defaultConfigFile
	oldDataDir := defaultDataDir
	oldLogDir := defaultLogDir
	oldRPCKeyFile := defaultRPCKeyFile
	oldRPCCertFile := defaultRPCCertFile
	oldArgs := os.Args
	oldActiveNetParams := activeNetParams
	oldChainActiveNetParams := chaincfg.ActiveNetParams
	oldInitLogRotatorFunc := initLogRotatorFunc
	oldParseAndSetDebugLevelsFunc := parseAndSetDebugLevelsFunc

	defaultHomeDir = filepath.Join(tempDir, "home")
	defaultConfigFile = filepath.Join(defaultHomeDir, defaultConfigFilename)
	defaultDataDir = filepath.Join(defaultHomeDir, defaultDataDirname)
	defaultLogDir = filepath.Join(defaultHomeDir, defaultLogDirname)
	defaultRPCKeyFile = filepath.Join(defaultHomeDir, "rpc.key")
	defaultRPCCertFile = filepath.Join(defaultHomeDir, "rpc.cert")

	activeNetParams = &chaincfg.Params{}
	*activeNetParams = chaincfg.MainNetParams
	chaincfg.ActiveNetParams = activeNetParams

	initLogRotatorFunc = func(string) {}
	parseAndSetDebugLevelsFunc = func(string) error { return nil }

	t.Cleanup(func() {
		defaultHomeDir = oldHomeDir
		defaultConfigFile = oldConfigFile
		defaultDataDir = oldDataDir
		defaultLogDir = oldLogDir
		defaultRPCKeyFile = oldRPCKeyFile
		defaultRPCCertFile = oldRPCCertFile
		os.Args = oldArgs
		activeNetParams = oldActiveNetParams
		chaincfg.ActiveNetParams = oldChainActiveNetParams
		initLogRotatorFunc = oldInitLogRotatorFunc
		parseAndSetDebugLevelsFunc = oldParseAndSetDebugLevelsFunc
	})

	return tempDir
}

func testGlobalParams(defaultPort, rpcPort string) chaincfg.GlobalParams {
	params := chaincfg.MainNetParams.GlobalParams
	params.DefaultPort = defaultPort
	params.RpcPort = rpcPort
	return params
}

func testConfig(dataDir, logDir string) *config {
	return &config{
		DataDir:        dataDir,
		LogDir:         logDir,
		DbType:         defaultDbType,
		DebugLevel:     defaultLogLevel,
		BanDuration:    defaultBanDuration,
		RPCUser:        "user",
		RPCPass:        "pass",
		MaxOrphanTxs:   defaultMaxOrphanTransactions,
		Generate:       false,
		GenerateMiner:  false,
		DisableBanning: false,
	}
}

func hasPort(addrs []string, port string) bool {
	suffix := ":" + port
	for _, addr := range addrs {
		if strings.HasSuffix(addr, suffix) {
			return true
		}
	}
	return false
}

func TestDeriveSVPDataBase(t *testing.T) {
	tempDir := withTestConfigGlobals(t)

	t.Run("default", func(t *testing.T) {
		mainData := filepath.Join(tempDir, "run", "data", "mainnet")
		base, err := deriveSVPDataBase(mainData, "")
		if err != nil {
			t.Fatalf("deriveSVPDataBase returned error: %v", err)
		}
		want := filepath.Join(tempDir, "run", "data")
		if base != want {
			t.Fatalf("base mismatch: got %q want %q", base, want)
		}
		final := filepath.Join(base, "4743546d")
		wantFinal := filepath.Join(tempDir, "run", "data", "4743546d")
		if final != wantFinal {
			t.Fatalf("final path mismatch: got %q want %q", final, wantFinal)
		}
	})

	t.Run("explicit", func(t *testing.T) {
		explicit := filepath.Join(tempDir, "svp-data")
		base, err := deriveSVPDataBase(filepath.Join(tempDir, "run", "data", "mainnet"), explicit)
		if err != nil {
			t.Fatalf("deriveSVPDataBase returned error: %v", err)
		}
		if base != explicit {
			t.Fatalf("base mismatch: got %q want %q", base, explicit)
		}
	})

	t.Run("degenerate", func(t *testing.T) {
		if _, err := deriveSVPDataBase(string(filepath.Separator), ""); err == nil {
			t.Fatal("expected degenerate main path error")
		}
	})
}

func TestApplyConfigDataNamespaceAndLogGating(t *testing.T) {
	tempDir := withTestConfigGlobals(t)

	initCount := 0
	debugCount := 0
	initLogRotatorFunc = func(string) { initCount++ }
	parseAndSetDebugLevelsFunc = func(string) error {
		debugCount++
		return nil
	}

	mainParams := testGlobalParams("9788", "9789")
	mainCfg := testConfig(filepath.Join(tempDir, "data"), filepath.Join(tempDir, "logs"))
	if err := applyConfig(mainCfg, &mainParams, applyConfigOptions{
		InitLogRotator:   true,
		ApplyDebugLevels: true,
		NormalizeLogDir:  true,
	}); err != nil {
		t.Fatalf("main applyConfig error: %v", err)
	}
	if got, want := mainCfg.DataDir, filepath.Join(tempDir, "data", "mainnet"); got != want {
		t.Fatalf("main data dir mismatch: got %q want %q", got, want)
	}
	if initCount != 1 || debugCount != 1 {
		t.Fatalf("main log/debug counts mismatch: init=%d debug=%d", initCount, debugCount)
	}

	initCount = 0
	debugCount = 0
	childParams := testGlobalParams("3788", "3789")
	childLogDir := filepath.Join(tempDir, "child-logs")
	childCfg := testConfig(filepath.Join(tempDir, "data"), childLogDir)
	if err := applyConfig(childCfg, &childParams, applyConfigOptions{
		DataDirNamespace: "4743546d",
		InitLogRotator:   false,
		ApplyDebugLevels: false,
		NormalizeLogDir:  false,
	}); err != nil {
		t.Fatalf("child applyConfig error: %v", err)
	}
	if got, want := childCfg.DataDir, filepath.Join(tempDir, "data", "4743546d"); got != want {
		t.Fatalf("child data dir mismatch: got %q want %q", got, want)
	}
	if childCfg.LogDir != childLogDir {
		t.Fatalf("child log dir was normalized: got %q want %q", childCfg.LogDir, childLogDir)
	}
	if initCount != 0 || debugCount != 0 {
		t.Fatalf("child log/debug counts mismatch: init=%d debug=%d", initCount, debugCount)
	}
}

func TestChildLoadConfigRuntimeIsolation(t *testing.T) {
	tempDir := withTestConfigGlobals(t)

	configFile := filepath.Join(tempDir, "omega.conf")
	if err := os.WriteFile(configFile, []byte{}, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runData := filepath.Join(tempDir, "run", "data")
	runLogs := filepath.Join(tempDir, "run", "logs")
	os.Args = []string{
		"omgd",
		"--datadir=" + runData,
		"--logdir=" + runLogs,
		"--listen=0.0.0.0:9788",
		"--rpclisten=127.0.0.1:9789",
		"--externalip=203.0.113.10:9788",
		"--rpcuser=u",
		"--rpcpass=p",
		"--testnet",
		"--configfile=" + configFile,
	}

	childParams := testGlobalParams("3788", "3789")
	cfg, _, err := loadConfigWithOptions("4743546d", 0x4743546d, nil, &childParams, loadConfigOptions{
		DataDirBase:         runData,
		DataDirNamespace:    "4743546d",
		RuntimeIsolation:    true,
		InitLogRotator:      false,
		ApplyDebugLevels:    false,
		NormalizeLogDir:     false,
		RejectChildGroupKey: true,
	})
	if err != nil {
		t.Fatalf("loadConfigWithOptions error: %v", err)
	}

	if got, want := cfg.DataDir, filepath.Join(runData, "4743546d"); got != want {
		t.Fatalf("child data dir mismatch: got %q want %q", got, want)
	}
	if !hasPort(cfg.Listeners, "3788") || hasPort(cfg.Listeners, "9788") {
		t.Fatalf("listener isolation failed: %v", cfg.Listeners)
	}
	if !hasPort(cfg.RPCListeners, "3789") || hasPort(cfg.RPCListeners, "9789") {
		t.Fatalf("rpc listener isolation failed: %v", cfg.RPCListeners)
	}
	if len(cfg.ExternalIPs) != 0 {
		t.Fatalf("external IPs were not cleared: %v", cfg.ExternalIPs)
	}
	if cfg.RPCUser != "u" || cfg.RPCPass != "p" {
		t.Fatalf("rpc credentials not preserved: user=%q pass=%q", cfg.RPCUser, cfg.RPCPass)
	}
	if cfg.TestNet || cfg.SimNet || cfg.RegressionTest {
		t.Fatalf("net flags not cleared: testnet=%v simnet=%v regtest=%v", cfg.TestNet, cfg.SimNet, cfg.RegressionTest)
	}
}

func TestChildConfigGroupRejectsRuntimeIsolatedKeys(t *testing.T) {
	for _, key := range []string{
		"datadir",
		"logdir",
		"listen",
		"rpclisten",
		"externalip",
		"testnet",
		"regtest",
		"simnet",
		"svpdatadir",
	} {
		t.Run(key, func(t *testing.T) {
			tempDir := withTestConfigGlobals(t)
			configFile := filepath.Join(tempDir, "omega.conf")
			contents := fmt.Sprintf("[4743546d]\n%s=value\n", key)
			if err := os.WriteFile(configFile, []byte(contents), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			os.Args = []string{
				"omgd",
				"--rpcuser=u",
				"--rpcpass=p",
				"--configfile=" + configFile,
			}
			childParams := testGlobalParams("3788", "3789")
			_, _, err := loadConfigWithOptions("4743546d", 0x4743546d, nil, &childParams, loadConfigOptions{
				DataDirBase:         filepath.Join(tempDir, "data"),
				DataDirNamespace:    "4743546d",
				RuntimeIsolation:    true,
				InitLogRotator:      false,
				ApplyDebugLevels:    false,
				NormalizeLogDir:     false,
				RejectChildGroupKey: true,
			})
			if err == nil {
				t.Fatalf("expected forbidden key %q error", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error %q does not name forbidden key %q", err, key)
			}
		})
	}
}

func TestDBPathsDoNotOverlap(t *testing.T) {
	mainCfg := &config{DataDir: filepath.Join("tmp", "run", "data", "mainnet")}
	childCfg := &config{DataDir: filepath.Join("tmp", "run", "data", "4743546d")}

	for _, dbType := range []string{"ffldb"} {
		t.Run(dbType, func(t *testing.T) {
			if blockDbPath(dbType, mainCfg) == blockDbPath(dbType, childCfg) {
				t.Fatalf("block db paths overlap for %s", dbType)
			}
			if minerDbPath(dbType, mainCfg) == minerDbPath(dbType, childCfg) {
				t.Fatalf("miner db paths overlap for %s", dbType)
			}
		})
	}
}

func TestRejectChildRuntimeConfigKeysIgnoresOtherSections(t *testing.T) {
	tempDir := withTestConfigGlobals(t)
	configFile := filepath.Join(tempDir, "omega.conf")
	contents := "[other]\nlisten=127.0.0.1:3788\n[4743546d]\n# listen=ignored\n"
	if err := os.WriteFile(configFile, []byte(contents), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := rejectChildRuntimeConfigKeys(configFile, "4743546d"); err != nil {
		t.Fatalf("unexpected reject error: %v", err)
	}
}

func TestChildLoadConfigRequiresIsolationFields(t *testing.T) {
	tempDir := withTestConfigGlobals(t)
	configFile := filepath.Join(tempDir, "omega.conf")
	if err := os.WriteFile(configFile, []byte{}, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	os.Args = []string{"omgd", "--rpcuser=u", "--rpcpass=p", "--configfile=" + configFile}
	childParams := testGlobalParams("3788", "3789")
	_, _, err := loadConfigWithOptions("4743546d", 0x4743546d, nil, &childParams, loadConfigOptions{
		RuntimeIsolation: true,
	})
	if err == nil {
		t.Fatal("expected missing isolation field error")
	}
}

func TestShouldStartSVPChildren(t *testing.T) {
	if !shouldStartSVPChildren(&config{NoSVP: false}) {
		t.Fatalf("default config should start SVP children")
	}
	if shouldStartSVPChildren(&config{NoSVP: true}) {
		t.Fatalf("--nosvp config must not start SVP children")
	}
}

func TestNoSVPFlagDefaults(t *testing.T) {
	tempDir := withTestConfigGlobals(t)
	configFile := filepath.Join(tempDir, "omega.conf")
	if err := os.WriteFile(configFile, []byte{}, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	os.Args = []string{
		"omgd",
		"--datadir=" + filepath.Join(tempDir, "run", "data"),
		"--logdir=" + filepath.Join(tempDir, "run", "logs"),
		"--rpcuser=u",
		"--rpcpass=p",
		"--configfile=" + configFile,
	}

	cfg, _, err := loadConfigWithOptions("Main Options", 0, nil, nil, loadConfigOptions{
		InitLogRotator:   false,
		ApplyDebugLevels: false,
		NormalizeLogDir:  false,
	})
	if err != nil {
		t.Fatalf("loadConfigWithOptions error: %v", err)
	}
	if cfg.NoSVP {
		t.Fatalf("default config must leave NoSVP false, got true")
	}
	if !shouldStartSVPChildren(cfg) {
		t.Fatalf("default config must start SVP children")
	}
}

func TestNoSVPFlagParses(t *testing.T) {
	tempDir := withTestConfigGlobals(t)
	configFile := filepath.Join(tempDir, "omega.conf")
	if err := os.WriteFile(configFile, []byte{}, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	os.Args = []string{
		"omgd",
		"--datadir=" + filepath.Join(tempDir, "run", "data"),
		"--logdir=" + filepath.Join(tempDir, "run", "logs"),
		"--rpcuser=u",
		"--rpcpass=p",
		"--nosvp",
		"--configfile=" + configFile,
	}

	cfg, _, err := loadConfigWithOptions("Main Options", 0, nil, nil, loadConfigOptions{
		InitLogRotator:   false,
		ApplyDebugLevels: false,
		NormalizeLogDir:  false,
	})
	if err != nil {
		t.Fatalf("loadConfigWithOptions error: %v", err)
	}
	if !cfg.NoSVP {
		t.Fatalf("--nosvp must set cfg.NoSVP=true")
	}
	if shouldStartSVPChildren(cfg) {
		t.Fatalf("--nosvp must skip SVP child startup")
	}
}

func TestNoSVPWithSVPDataDirIsSilentlyInert(t *testing.T) {
	tempDir := withTestConfigGlobals(t)
	configFile := filepath.Join(tempDir, "omega.conf")
	if err := os.WriteFile(configFile, []byte{}, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	svpDataDir := filepath.Join(tempDir, "operator-svp")
	os.Args = []string{
		"omgd",
		"--datadir=" + filepath.Join(tempDir, "run", "data"),
		"--logdir=" + filepath.Join(tempDir, "run", "logs"),
		"--rpcuser=u",
		"--rpcpass=p",
		"--nosvp",
		"--svpdatadir=" + svpDataDir,
		"--configfile=" + configFile,
	}

	cfg, _, err := loadConfigWithOptions("Main Options", 0, nil, nil, loadConfigOptions{
		InitLogRotator:   false,
		ApplyDebugLevels: false,
		NormalizeLogDir:  false,
	})
	if err != nil {
		t.Fatalf("loadConfigWithOptions error: %v", err)
	}
	if !cfg.NoSVP {
		t.Fatalf("--nosvp must set cfg.NoSVP=true")
	}
	if cfg.SVPDataDir != svpDataDir {
		t.Fatalf("--svpdatadir not preserved: got %q want %q", cfg.SVPDataDir, svpDataDir)
	}
	if shouldStartSVPChildren(cfg) {
		t.Fatalf("--nosvp must keep child startup gated even when --svpdatadir is set")
	}
	if _, err := os.Stat(svpDataDir); !os.IsNotExist(err) {
		t.Fatalf("svp data dir should not have been created under --nosvp: stat err=%v", err)
	}
}
