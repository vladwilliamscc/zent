package minerchain

import (
	"sync"
	"testing"
	"time"

	"btcd/blockchain"
	"btcd/mining"
	"btcd/wire"
	"btcd/wire/common"
	"btcutil"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
	"omega/token"
	"omega/viewpoint"
)

type fakeCollateralBackend struct {
	mu       sync.Mutex
	utxos    map[wire.OutPoint]*viewpoint.UtxoEntry
	height   int32
	hash     chainhash.Hash
	required int64
	fetches  int
}

func newFakeCollateralBackend(required int64) *fakeCollateralBackend {
	f := &fakeCollateralBackend{
		utxos:    make(map[wire.OutPoint]*viewpoint.UtxoEntry),
		height:   10,
		required: required,
	}
	f.hash[0] = 1
	return f
}

func (f *fakeCollateralBackend) backend() collateralCacheBackend {
	return collateralCacheBackend{
		fetchUtxo: func(op wire.OutPoint) (*viewpoint.UtxoEntry, error) {
			f.mu.Lock()
			defer f.mu.Unlock()

			f.fetches++
			entry := f.utxos[op]
			if entry == nil {
				return nil, nil
			}
			return entry.Clone(), nil
		},
		bestMinerSnapshot: func() *blockchain.BestState {
			f.mu.Lock()
			defer f.mu.Unlock()
			return &blockchain.BestState{Hash: f.hash, Height: f.height}
		},
		requiredAmount: func() int64 {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.required
		},
		now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return time.Unix(1000+int64(f.fetches), 0)
		},
	}
}

func (f *fakeCollateralBackend) setUTXO(op wire.OutPoint, owner [20]byte, amount int64, tokenType uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.utxos[op] = testCollateralUTXO(op, owner, amount, tokenType)
}

func (f *fakeCollateralBackend) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

func testOutPoint(seed byte, index uint32) wire.OutPoint {
	var hash chainhash.Hash
	hash[0] = seed
	hash[31] = seed + 1
	return wire.OutPoint{Hash: hash, Index: index}
}

func testOwner(seed byte) [20]byte {
	var owner [20]byte
	for i := range owner {
		owner[i] = seed
	}
	return owner
}

func testPkScript(owner [20]byte) []byte {
	pkScript := make([]byte, collateralOwnerScriptLen)
	pkScript[0] = 0
	copy(pkScript[1:], owner[:])
	return pkScript
}

func testCollateralUTXO(op wire.OutPoint, owner [20]byte, amount int64, tokenType uint64) *viewpoint.UtxoEntry {
	view := viewpoint.NewUtxoViewpoint()
	txOut := wire.NewTxOut(tokenType, &token.NumToken{Val: amount}, nil, testPkScript(owner))
	return view.AddRawTxOut(op, txOut, false, 1)
}

func testMinerWithCollateralCache(cache *CollateralCache) *CPUMiner {
	return &CPUMiner{
		g: &mining.BlkTmplGenerator{
			Collateral: make(map[[20]byte][]*wire.OutPoint),
		},
		collateralCache: cache,
	}
}

func assertEntryState(t *testing.T, cache *CollateralCache, op wire.OutPoint, want CollateralState) CollateralEntry {
	t.Helper()
	entry, ok := cache.Entry(op)
	if !ok {
		t.Fatalf("missing cache entry for %v", op)
	}
	if entry.State != want {
		t.Fatalf("entry state mismatch for %v: got %v want %v", op, entry.State, want)
	}
	return entry
}

func TestCollateralCacheAddAndDropHooks(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(1)
	op := testOutPoint(1, 0)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(op, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend(), owner)
	miner := testMinerWithCollateralCache(cache)

	miner.AddCollateral(op.Hash, op.Index)

	collaterals := miner.g.Collateral[owner]
	if len(collaterals) != 1 || collaterals[0] == nil || !collaterals[0].Equal(&op) {
		t.Fatalf("g.Collateral was not updated with added outpoint: %#v", collaterals)
	}

	entry := assertEntryState(t, cache, op, StateEligible)
	if entry.Amount != int64(150)*collateralHaoPerCoin {
		t.Fatalf("entry amount mismatch: got %d", entry.Amount)
	}
	if entry.TokenType != common.FeeCoinTyp {
		t.Fatalf("entry token mismatch: got %d", entry.TokenType)
	}
	if entry.OwnerHash != owner {
		t.Fatalf("entry owner mismatch: got %x want %x", entry.OwnerHash, owner)
	}

	miner.DropCollateral(op.Hash, op.Index)

	if _, ok := cache.Entry(op); ok {
		t.Fatalf("cache entry still present after DropCollateral")
	}
	if len(miner.g.Collateral[owner]) != 0 {
		t.Fatalf("g.Collateral still contains dropped outpoint: %#v", miner.g.Collateral[owner])
	}
}

func TestCollateralCacheStartupLoadStates(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(2)
	otherOwner := testOwner(3)
	opEligible := testOutPoint(2, 0)
	opMissing := testOutPoint(3, 0)
	opToken := testOutPoint(4, 0)
	opOwner := testOutPoint(5, 0)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(opEligible, owner, int64(200)*collateralHaoPerCoin, common.FeeCoinTyp)
	fake.setUTXO(opToken, owner, int64(200)*collateralHaoPerCoin, common.FeeCoinTyp+1)
	fake.setUTXO(opOwner, otherOwner, int64(200)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend())
	audit := cache.LoadFromCollateralMap(map[[20]byte][]*wire.OutPoint{
		owner: {&opEligible, &opMissing, &opToken, &opOwner},
	})

	if audit.Loaded != 1 || audit.NotFound != 1 || audit.TokenMismatch != 1 || audit.OwnerMismatch != 1 {
		t.Fatalf("startup audit mismatch: %#v", audit)
	}

	assertEntryState(t, cache, opEligible, StateEligible)
	assertEntryState(t, cache, opMissing, StateNotFound)
	assertEntryState(t, cache, opToken, StateTokenMismatch)
	assertEntryState(t, cache, opOwner, StateOwnerMismatch)
}

func TestCollateralCacheTxChainNotifications(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(4)
	op := testOutPoint(6, 1)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(op, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend(), owner)
	miner := testMinerWithCollateralCache(cache)
	if _, err := cache.HookAddCollateral(op); err != nil {
		t.Fatalf("HookAddCollateral error: %v", err)
	}

	block := testTxBlockSpending(op)
	miner.NoticeForCacheTx(&blockchain.Notification{
		Type: blockchain.NTBlockConnected,
		Data: block,
	})
	assertEntryState(t, cache, op, StateSpent)

	miner.NoticeForCacheTx(&blockchain.Notification{
		Type: blockchain.NTBlockDisconnected,
		Data: block,
	})
	assertEntryState(t, cache, op, StateStale)

	stats := cache.Stats()
	if stats.ExternalSpendDetectedTotal != 1 {
		t.Fatalf("external spend counter mismatch: got %d", stats.ExternalSpendDetectedTotal)
	}
	if stats.ReorgInvalidationsTotal["tx"] != 1 {
		t.Fatalf("tx reorg counter mismatch: %#v", stats.ReorgInvalidationsTotal)
	}
}

func TestCollateralCacheMinerChainNotifications(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(5)
	op := testOutPoint(7, 1)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(op, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend(), owner)
	miner := testMinerWithCollateralCache(cache)
	if _, err := cache.HookAddCollateral(op); err != nil {
		t.Fatalf("HookAddCollateral error: %v", err)
	}

	miner.NoticeForCacheMR(&blockchain.Notification{
		Type: blockchain.NTBlockConnected,
		Data: testMinerBlockUsing(op, 40),
	})
	miner.NoticeForCacheMR(&blockchain.Notification{
		Type: blockchain.NTBlockConnected,
		Data: testMinerBlockUsing(op, 42),
	})

	entry := assertEntryState(t, cache, op, StateEligible)
	if entry.LastUsedMinerHeight != 42 {
		t.Fatalf("last used height mismatch: got %d want 42", entry.LastUsedMinerHeight)
	}

	miner.NoticeForCacheMR(&blockchain.Notification{
		Type: blockchain.NTBlockDisconnected,
		Data: testMinerBlockUsing(op, 42),
	})
	entry = assertEntryState(t, cache, op, StateStale)
	if entry.LastUsedMinerHeight != 40 {
		t.Fatalf("last used rollback mismatch: got %d want 40", entry.LastUsedMinerHeight)
	}

	miner.NoticeForCacheMR(&blockchain.Notification{
		Type: blockchain.NTBlockDisconnected,
		Data: testMinerBlockUsing(op, 40),
	})
	entry = assertEntryState(t, cache, op, StateStale)
	if entry.LastUsedMinerHeight != 0 {
		t.Fatalf("last used clear mismatch: got %d want 0", entry.LastUsedMinerHeight)
	}
}

func TestCollateralCacheRefreshStates(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(6)
	otherOwner := testOwner(7)
	opMissing := testOutPoint(8, 0)
	opToken := testOutPoint(9, 0)
	opOwner := testOutPoint(10, 0)
	opAmount := testOutPoint(11, 0)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(opToken, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp+1)
	fake.setUTXO(opOwner, otherOwner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)
	fake.setUTXO(opAmount, owner, int64(50)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend(), owner)

	if _, err := cache.HookAddCollateral(opMissing); err != nil {
		t.Fatalf("missing refresh returned error: %v", err)
	}
	if _, err := cache.HookAddCollateral(opToken); err != nil {
		t.Fatalf("token refresh returned error: %v", err)
	}
	if _, err := cache.HookAddCollateral(opOwner); err != nil {
		t.Fatalf("owner refresh returned error: %v", err)
	}
	if _, err := cache.HookAddCollateral(opAmount); err != nil {
		t.Fatalf("amount refresh returned error: %v", err)
	}

	assertEntryState(t, cache, opMissing, StateNotFound)
	assertEntryState(t, cache, opToken, StateTokenMismatch)
	assertEntryState(t, cache, opOwner, StateOwnerMismatch)
	assertEntryState(t, cache, opAmount, StateAmountInsufficient)
}

func TestCollateralCacheRefreshIfStale(t *testing.T) {
	const required = int64(100) * collateralHaoPerCoin
	owner := testOwner(8)
	op := testOutPoint(12, 0)

	fake := newFakeCollateralBackend(required)
	fake.setUTXO(op, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)

	cache := newCollateralCacheWithExpectedOwners(fake.backend(), owner)
	if _, err := cache.HookAddCollateral(op); err != nil {
		t.Fatalf("HookAddCollateral error: %v", err)
	}

	fetches := fake.fetchCount()
	entry, err := cache.RefreshIfStale(op, 50)
	if err != nil {
		t.Fatalf("RefreshIfStale error: %v", err)
	}
	if entry.State != StateEligible {
		t.Fatalf("unexpected state without stale refresh: %v", entry.State)
	}
	if fake.fetchCount() != fetches {
		t.Fatalf("non-stale refresh fetched unexpectedly: got %d want %d", fake.fetchCount(), fetches)
	}

	fake.setUTXO(op, owner, int64(50)*collateralHaoPerCoin, common.FeeCoinTyp)
	entry, err = cache.RefreshIfStale(op, 0)
	if err != nil {
		t.Fatalf("zero-ttl RefreshIfStale error: %v", err)
	}
	if entry.State != StateAmountInsufficient {
		t.Fatalf("zero-ttl refresh did not force amount update: got %v", entry.State)
	}

	fake.setUTXO(op, owner, int64(150)*collateralHaoPerCoin, common.FeeCoinTyp)
	if !cache.MarkStale(op) {
		t.Fatalf("MarkStale failed")
	}
	entry, err = cache.RefreshIfStale(op, 50)
	if err != nil {
		t.Fatalf("stale RefreshIfStale error: %v", err)
	}
	if entry.State != StateEligible {
		t.Fatalf("stale refresh did not restore eligibility: got %v", entry.State)
	}
}

func testTxBlockSpending(op wire.OutPoint) *btcutil.Block {
	tx := wire.NewMsgTx(wire.TxVersion)
	tx.AddTxIn(wire.NewTxIn(&op, 0))
	block := &wire.MsgBlock{}
	block.AddTransaction(tx)
	return btcutil.NewBlock(block)
}

func testMinerBlockUsing(op wire.OutPoint, height int32) *wire.MinerBlock {
	opCopy := op
	block := wire.NewMinerBlock(&wire.MingingRightBlock{
		Utxos: &opCopy,
	})
	block.SetHeight(height)
	return block
}
