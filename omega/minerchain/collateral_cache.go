package minerchain

import (
	"fmt"
	"sync"
	"time"

	"btcd/blockchain"
	"btcd/database"
	"btcd/mining"
	"btcd/wire"
	"btcd/wire/common"
	"btcutil"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
	"omega/viewpoint"
)

const (
	collateralOwnerScriptLen = 21
	collateralHaoPerCoin     = int64(1e8)
)

// CollateralState describes the cache-side validity state for one collateral
// outpoint.  It is deliberately separate from consensus validation; PR-3 keeps
// mining selection behavior on the existing path.
type CollateralState int8

const (
	StateUnknown CollateralState = iota
	StateEligible
	StateAmountInsufficient
	StateTokenMismatch
	StateOwnerMismatch
	StateSpent
	StateNotFound
	StateStale
)

func (s CollateralState) String() string {
	switch s {
	case StateUnknown:
		return "unknown"
	case StateEligible:
		return "eligible"
	case StateAmountInsufficient:
		return "amount_insufficient"
	case StateTokenMismatch:
		return "token_mismatch"
	case StateOwnerMismatch:
		return "owner_mismatch"
	case StateSpent:
		return "spent"
	case StateNotFound:
		return "not_found"
	case StateStale:
		return "stale"
	default:
		return fmt.Sprintf("unknown_state_%d", int8(s))
	}
}

type CollateralEntry struct {
	OutPoint            wire.OutPoint
	Amount              int64
	TokenType           uint64
	OwnerHash           [20]byte
	LastCheckedHash     chainhash.Hash
	LastCheckedHeight   int32
	State               CollateralState
	LastErrorReason     string
	LastRefreshAt       time.Time
	LastUsedMinerHeight int32
}

type CollateralStartupAudit struct {
	Loaded        int
	NotFound      int
	TokenMismatch int
	OwnerMismatch int
}

type CollateralCacheStats struct {
	StateCounts                map[CollateralState]int
	InvalidationTotal          map[string]uint64
	ExternalSpendDetectedTotal uint64
	MRReuseDetectedTotal       uint64
	ReorgInvalidationsTotal    map[string]uint64
	StartupLoadAuditTotal      CollateralStartupAudit
}

type collateralCacheBackend struct {
	fetchUtxo         func(wire.OutPoint) (*viewpoint.UtxoEntry, error)
	bestMinerSnapshot func() *blockchain.BestState
	requiredAmount    func() int64
	now               func() time.Time
}

type CollateralCache struct {
	mu             sync.RWMutex
	entries        map[wire.OutPoint]CollateralEntry
	reuseHistory   map[wire.OutPoint][]int32
	expectedOwners map[[20]byte]struct{}
	backend        collateralCacheBackend
	stats          CollateralCacheStats
}

func NewCollateralCache(g *mining.BlkTmplGenerator, miningAddrs []btcutil.Address) *CollateralCache {
	return newCollateralCacheWithBackend(collateralBackendFromGenerator(g), miningAddrs)
}

func newCollateralCacheWithBackend(backend collateralCacheBackend, miningAddrs []btcutil.Address) *CollateralCache {
	owners := make([][20]byte, 0, len(miningAddrs))
	for _, addr := range miningAddrs {
		if addr == nil {
			continue
		}
		var owner [20]byte
		copy(owner[:], addr.ScriptAddress())
		owners = append(owners, owner)
	}
	return newCollateralCacheWithExpectedOwners(backend, owners...)
}

func newCollateralCacheWithExpectedOwners(backend collateralCacheBackend, owners ...[20]byte) *CollateralCache {
	if backend.fetchUtxo == nil {
		backend.fetchUtxo = func(wire.OutPoint) (*viewpoint.UtxoEntry, error) {
			return nil, fmt.Errorf("collateral cache fetch backend is unavailable")
		}
	}
	if backend.bestMinerSnapshot == nil {
		backend.bestMinerSnapshot = func() *blockchain.BestState { return nil }
	}
	if backend.requiredAmount == nil {
		backend.requiredAmount = func() int64 { return 0 }
	}
	if backend.now == nil {
		backend.now = time.Now
	}

	expectedOwners := make(map[[20]byte]struct{}, len(owners))
	for _, owner := range owners {
		expectedOwners[owner] = struct{}{}
	}
	if len(expectedOwners) == 0 {
		expectedOwners = nil
	}

	return &CollateralCache{
		entries:        make(map[wire.OutPoint]CollateralEntry),
		reuseHistory:   make(map[wire.OutPoint][]int32),
		expectedOwners: expectedOwners,
		backend:        backend,
		stats: CollateralCacheStats{
			InvalidationTotal:       make(map[string]uint64),
			ReorgInvalidationsTotal: make(map[string]uint64),
		},
	}
}

func collateralBackendFromGenerator(g *mining.BlkTmplGenerator) collateralCacheBackend {
	return collateralCacheBackend{
		fetchUtxo: func(op wire.OutPoint) (*viewpoint.UtxoEntry, error) {
			if g == nil || g.Chain == nil {
				return nil, fmt.Errorf("collateral cache has no tx-chain")
			}
			return g.Chain.FetchUtxoEntry(op)
		},
		bestMinerSnapshot: func() *blockchain.BestState {
			if g == nil || g.Chain == nil || g.Chain.Miners == nil {
				return nil
			}
			return g.Chain.Miners.BestSnapshot()
		},
		requiredAmount: func() int64 {
			return collateralRequiredAmountFromGenerator(g)
		},
		now: time.Now,
	}
}

func collateralRequiredAmountFromGenerator(g *mining.BlkTmplGenerator) int64 {
	if g == nil || g.Chain == nil || g.Chain.Miners == nil {
		return 0
	}

	if tip := g.Chain.Miners.Tip(); tip != nil && tip.MsgBlock() != nil {
		return int64(tip.MsgBlock().Collateral) * collateralHaoPerCoin
	}

	best := g.Chain.Miners.BestSnapshot()
	if best == nil {
		return 0
	}
	block, err := g.Chain.Miners.BlockByHeight(best.Height)
	if err != nil || block == nil || block.MsgBlock() == nil {
		return 0
	}
	return int64(block.MsgBlock().Collateral) * collateralHaoPerCoin
}

func (c *CollateralCache) HookAddCollateral(op wire.OutPoint) (CollateralEntry, error) {
	return c.refreshOne(op, nil)
}

func (c *CollateralCache) HookDropCollateral(op wire.OutPoint) {
	c.Remove(op)
}

func (c *CollateralCache) Add(op wire.OutPoint) (CollateralEntry, error) {
	return c.HookAddCollateral(op)
}

func (c *CollateralCache) Remove(op wire.OutPoint) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, op)
	delete(c.reuseHistory, op)
	c.mu.Unlock()
}

func (c *CollateralCache) RefreshOne(op wire.OutPoint) (CollateralEntry, error) {
	return c.refreshOne(op, nil)
}

func (c *CollateralCache) RefreshIfStale(op wire.OutPoint, ttlBlocks int32) (CollateralEntry, error) {
	if c == nil {
		return CollateralEntry{}, fmt.Errorf("collateral cache is nil")
	}
	if ttlBlocks <= 0 {
		return c.RefreshOne(op)
	}

	best := c.backend.bestMinerSnapshot()

	c.mu.RLock()
	entry, ok := c.entries[op]
	c.mu.RUnlock()
	if !ok {
		return c.RefreshOne(op)
	}
	if entry.State == StateUnknown || entry.State == StateStale {
		return c.RefreshOne(op)
	}
	if best == nil {
		return entry, nil
	}
	if best.Height < entry.LastCheckedHeight {
		return c.RefreshOne(op)
	}
	if best.Height == entry.LastCheckedHeight && !entry.LastCheckedHash.IsEqual(&best.Hash) {
		return c.RefreshOne(op)
	}
	if best.Height-entry.LastCheckedHeight > ttlBlocks {
		return c.RefreshOne(op)
	}

	return entry, nil
}

func (c *CollateralCache) PickEligible(owner [20]byte, requiredAmount int64, excluded map[wire.OutPoint]struct{}) []wire.OutPoint {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var picks []wire.OutPoint
	for op, entry := range c.entries {
		if entry.State != StateEligible {
			continue
		}
		if entry.OwnerHash != owner {
			continue
		}
		if entry.TokenType != common.FeeCoinTyp {
			continue
		}
		if entry.Amount < requiredAmount {
			continue
		}
		if _, ok := excluded[op]; ok {
			continue
		}
		picks = append(picks, op)
	}

	return picks
}

func (c *CollateralCache) MarkSpent(op wire.OutPoint) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[op]
	if !ok {
		return false
	}
	entry.State = StateSpent
	entry.LastErrorReason = "spent"
	c.entries[op] = entry
	c.stats.InvalidationTotal["spent"]++
	return true
}

func (c *CollateralCache) MarkStale(op wire.OutPoint) bool {
	return c.markStale(op, "stale", "")
}

func (c *CollateralCache) MarkMinerReuse(op wire.OutPoint, height int32) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[op]
	if !ok {
		return false
	}

	history := c.reuseHistory[op]
	seen := false
	for _, h := range history {
		if h == height {
			seen = true
			break
		}
	}
	if !seen {
		history = append(history, height)
		c.reuseHistory[op] = history
	}

	entry.LastUsedMinerHeight = maxMinerReuseHeight(history)
	c.entries[op] = entry
	c.stats.MRReuseDetectedTotal++
	return true
}

func (c *CollateralCache) ClearMinerReuse(op wire.OutPoint, height int32) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[op]
	if !ok {
		return false
	}

	history := c.reuseHistory[op]
	filtered := history[:0]
	for _, h := range history {
		if h != height {
			filtered = append(filtered, h)
		}
	}
	if len(filtered) == 0 {
		delete(c.reuseHistory, op)
		entry.LastUsedMinerHeight = 0
	} else {
		c.reuseHistory[op] = filtered
		entry.LastUsedMinerHeight = maxMinerReuseHeight(filtered)
	}

	entry.State = StateStale
	entry.LastErrorReason = "miner-chain disconnect"
	c.entries[op] = entry
	c.stats.InvalidationTotal["stale"]++
	c.stats.ReorgInvalidationsTotal["mr"]++
	return true
}

func (c *CollateralCache) Snapshot() map[wire.OutPoint]CollateralEntry {
	result := make(map[wire.OutPoint]CollateralEntry)
	if c == nil {
		return result
	}
	c.mu.RLock()
	for op, entry := range c.entries {
		result[op] = entry
	}
	c.mu.RUnlock()
	return result
}

func (c *CollateralCache) Entry(op wire.OutPoint) (CollateralEntry, bool) {
	if c == nil {
		return CollateralEntry{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[op]
	c.mu.RUnlock()
	return entry, ok
}

func (c *CollateralCache) Stats() CollateralCacheStats {
	stats := CollateralCacheStats{
		StateCounts:                make(map[CollateralState]int),
		InvalidationTotal:          make(map[string]uint64),
		ExternalSpendDetectedTotal: 0,
		MRReuseDetectedTotal:       0,
		ReorgInvalidationsTotal:    make(map[string]uint64),
		StartupLoadAuditTotal:      CollateralStartupAudit{},
	}
	if c == nil {
		return stats
	}

	c.mu.RLock()
	for _, entry := range c.entries {
		stats.StateCounts[entry.State]++
	}
	for reason, count := range c.stats.InvalidationTotal {
		stats.InvalidationTotal[reason] = count
	}
	for chain, count := range c.stats.ReorgInvalidationsTotal {
		stats.ReorgInvalidationsTotal[chain] = count
	}
	stats.ExternalSpendDetectedTotal = c.stats.ExternalSpendDetectedTotal
	stats.MRReuseDetectedTotal = c.stats.MRReuseDetectedTotal
	stats.StartupLoadAuditTotal = c.stats.StartupLoadAuditTotal
	c.mu.RUnlock()

	return stats
}

func (c *CollateralCache) LoadFromCollateralMap(collateral map[[20]byte][]*wire.OutPoint) CollateralStartupAudit {
	var audit CollateralStartupAudit
	if c == nil {
		return audit
	}

	for owner, ops := range collateral {
		expectedOwner := owner
		for _, op := range ops {
			if op == nil {
				continue
			}
			entry, _ := c.refreshOne(*op, &expectedOwner)
			audit.addState(entry.State)
		}
	}

	c.mu.Lock()
	c.stats.StartupLoadAuditTotal.Loaded += audit.Loaded
	c.stats.StartupLoadAuditTotal.NotFound += audit.NotFound
	c.stats.StartupLoadAuditTotal.TokenMismatch += audit.TokenMismatch
	c.stats.StartupLoadAuditTotal.OwnerMismatch += audit.OwnerMismatch
	c.mu.Unlock()

	return audit
}

func (a *CollateralStartupAudit) addState(state CollateralState) {
	switch state {
	case StateNotFound:
		a.NotFound++
	case StateTokenMismatch:
		a.TokenMismatch++
	case StateOwnerMismatch:
		a.OwnerMismatch++
	default:
		a.Loaded++
	}
}

func (c *CollateralCache) refreshOne(op wire.OutPoint, expectedOwner *[20]byte) (CollateralEntry, error) {
	if c == nil {
		return CollateralEntry{}, fmt.Errorf("collateral cache is nil")
	}

	entry, err := c.buildEntry(op, expectedOwner)

	c.mu.Lock()
	if old, ok := c.entries[op]; ok {
		entry.LastUsedMinerHeight = old.LastUsedMinerHeight
	}
	c.entries[op] = entry
	c.mu.Unlock()

	return entry, err
}

func (c *CollateralCache) buildEntry(op wire.OutPoint, expectedOwner *[20]byte) (CollateralEntry, error) {
	entry := CollateralEntry{
		OutPoint:      op,
		State:         StateUnknown,
		LastRefreshAt: c.backend.now(),
	}

	if best := c.backend.bestMinerSnapshot(); best != nil {
		entry.LastCheckedHash = best.Hash
		entry.LastCheckedHeight = best.Height
	}

	utxo, err := c.backend.fetchUtxo(op)
	if err != nil {
		entry.LastErrorReason = err.Error()
		return entry, err
	}
	if utxo == nil {
		entry.State = StateNotFound
		entry.LastErrorReason = "utxo not found"
		return entry, nil
	}

	entry.TokenType = utxo.TokenType
	entry.Amount = utxo.NumAmount()

	owner, ok := collateralOwnerFromPkScript(utxo.PkScript())
	if !ok {
		entry.State = StateOwnerMismatch
		entry.LastErrorReason = "pkScript owner hash unavailable"
		return entry, nil
	}
	entry.OwnerHash = owner

	if entry.TokenType != common.FeeCoinTyp {
		entry.State = StateTokenMismatch
		entry.LastErrorReason = "token type mismatch"
		return entry, nil
	}

	requiredAmount := c.backend.requiredAmount()
	if requiredAmount > 0 && entry.Amount < requiredAmount {
		entry.State = StateAmountInsufficient
		entry.LastErrorReason = "amount insufficient"
		return entry, nil
	}

	if expectedOwner != nil {
		if entry.OwnerHash != *expectedOwner {
			entry.State = StateOwnerMismatch
			entry.LastErrorReason = "owner mismatch"
			return entry, nil
		}
	} else if len(c.expectedOwners) > 0 {
		if _, ok := c.expectedOwners[entry.OwnerHash]; !ok {
			entry.State = StateOwnerMismatch
			entry.LastErrorReason = "owner mismatch"
			return entry, nil
		}
	}

	entry.State = StateEligible
	return entry, nil
}

func collateralOwnerFromPkScript(pkScript []byte) ([20]byte, bool) {
	var owner [20]byte
	if len(pkScript) < collateralOwnerScriptLen {
		return owner, false
	}
	copy(owner[:], pkScript[1:collateralOwnerScriptLen])
	return owner, true
}

func maxMinerReuseHeight(history []int32) int32 {
	var max int32
	for _, height := range history {
		if height > max {
			max = height
		}
	}
	return max
}

func (c *CollateralCache) markStale(op wire.OutPoint, reason, chain string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[op]
	if !ok {
		return false
	}
	entry.State = StateStale
	entry.LastErrorReason = reason
	c.entries[op] = entry
	c.stats.InvalidationTotal["stale"]++
	if chain != "" {
		c.stats.ReorgInvalidationsTotal[chain]++
	}
	return true
}

func (c *CollateralCache) recordExternalSpend() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stats.ExternalSpendDetectedTotal++
	c.mu.Unlock()
}

func (m *CPUMiner) ensureCollateralCache() *CollateralCache {
	if m == nil {
		return nil
	}
	m.collateralCacheMu.Lock()
	defer m.collateralCacheMu.Unlock()
	if m.collateralCache == nil {
		m.collateralCache = NewCollateralCache(m.g, m.cfg.MiningAddrs)
	}
	return m.collateralCache
}

func (m *CPUMiner) initCollateralCache() {
	cache := m.ensureCollateralCache()
	if cache == nil {
		return
	}

	var audit CollateralStartupAudit
	if m.g != nil && m.g.Collateral != nil {
		audit = cache.LoadFromCollateralMap(m.g.Collateral)
	}
	log.Infof("collateral cache startup audit: loaded=%d not_found=%d token_mismatch=%d owner_mismatch=%d",
		audit.Loaded, audit.NotFound, audit.TokenMismatch, audit.OwnerMismatch)

	if m.g == nil || m.g.Chain == nil {
		return
	}
	m.g.Chain.Subscribe(m.NoticeForCacheTx)
	if m.g.Chain.Miners != nil {
		m.g.Chain.Miners.Subscribe(m.NoticeForCacheMR)
	}
}

func (m *CPUMiner) loadPersistedCollaterals() CollateralStartupAudit {
	var audit CollateralStartupAudit
	if m == nil || m.g == nil || m.g.Chain == nil || m.g.Chain.Miners == nil {
		return audit
	}

	miners, ok := m.g.Chain.Miners.(*MinerChain)
	if !ok || miners.db == nil {
		return audit
	}

	ops := make([]wire.OutPoint, 0)
	err := miners.db.View(func(dbTx database.Tx) error {
		bucket := dbTx.Metadata().Bucket([]byte(common.MiningCollaterals))
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for ok := cursor.First(); ok; ok = cursor.Next() {
			key := cursor.Key()
			if len(key) < 36 {
				continue
			}
			var op wire.OutPoint
			copy(op.Hash[:], key[:32])
			op.Index = common.LittleEndian.Uint32(key[32:36])
			ops = append(ops, op)
		}
		return nil
	})
	if err != nil {
		log.Warnf("collateral cache persisted load failed: %v", err)
		return audit
	}

	cache := m.ensureCollateralCache()
	for _, op := range ops {
		entry, err := cache.HookAddCollateral(op)
		if err != nil {
			continue
		}
		audit.addState(entry.State)
		if collateralEntryHasOwner(entry) {
			m.appendCollateral(entry.OwnerHash, op, true)
		}
	}

	cache.mu.Lock()
	cache.stats.StartupLoadAuditTotal.Loaded += audit.Loaded
	cache.stats.StartupLoadAuditTotal.NotFound += audit.NotFound
	cache.stats.StartupLoadAuditTotal.TokenMismatch += audit.TokenMismatch
	cache.stats.StartupLoadAuditTotal.OwnerMismatch += audit.OwnerMismatch
	cache.mu.Unlock()

	return audit
}

func (m *CPUMiner) appendCollateral(owner [20]byte, op wire.OutPoint, dedupe bool) {
	if m == nil || m.g == nil {
		return
	}

	m.submitBlockLock.Lock()
	defer m.submitBlockLock.Unlock()

	if m.g.Collateral == nil {
		m.g.Collateral = make(map[[20]byte][]*wire.OutPoint)
	}
	if dedupe {
		for _, existing := range m.g.Collateral[owner] {
			if existing != nil && existing.Equal(&op) {
				return
			}
		}
	}

	copied := op
	m.g.Collateral[owner] = append(m.g.Collateral[owner], &copied)
}

func (m *CPUMiner) removeCollateral(op wire.OutPoint) bool {
	if m == nil || m.g == nil {
		return false
	}

	m.submitBlockLock.Lock()
	defer m.submitBlockLock.Unlock()

	for owner, collaterals := range m.g.Collateral {
		for i, existing := range collaterals {
			if existing != nil && existing.Equal(&op) {
				m.g.Collateral[owner] = append(collaterals[:i], collaterals[i+1:]...)
				return true
			}
		}
	}
	return false
}

func collateralEntryHasOwner(entry CollateralEntry) bool {
	return entry.State != StateNotFound &&
		!(entry.State == StateUnknown && entry.LastErrorReason != "") &&
		entry.LastErrorReason != "pkScript owner hash unavailable"
}

func (m *CPUMiner) NoticeForCacheTx(notification *blockchain.Notification) {
	if m == nil || m.collateralCache == nil || notification == nil {
		return
	}

	block, ok := notification.Data.(*btcutil.Block)
	if !ok || block == nil {
		return
	}

	switch notification.Type {
	case blockchain.NTBlockConnected:
		for _, tx := range block.Transactions() {
			if tx == nil || tx.MsgTx() == nil {
				continue
			}
			for _, txIn := range tx.MsgTx().TxIn {
				if txIn == nil {
					continue
				}
				if m.collateralCache.MarkSpent(txIn.PreviousOutPoint) {
					m.collateralCache.recordExternalSpend()
				}
			}
		}
	case blockchain.NTBlockDisconnected:
		for _, tx := range block.Transactions() {
			if tx == nil || tx.MsgTx() == nil {
				continue
			}
			for _, txIn := range tx.MsgTx().TxIn {
				if txIn == nil {
					continue
				}
				m.collateralCache.markStale(txIn.PreviousOutPoint, "tx-chain disconnect", "tx")
			}
		}
	}
}

func (m *CPUMiner) NoticeForCacheMR(notification *blockchain.Notification) {
	if m == nil || m.collateralCache == nil || notification == nil {
		return
	}

	block, ok := notification.Data.(*wire.MinerBlock)
	if !ok || block == nil || block.MsgBlock() == nil || block.MsgBlock().Utxos == nil {
		return
	}

	height := block.Height()
	if height < 0 {
		if best := m.collateralCache.backend.bestMinerSnapshot(); best != nil {
			height = best.Height
		}
	}

	switch notification.Type {
	case blockchain.NTBlockConnected:
		m.collateralCache.MarkMinerReuse(*block.MsgBlock().Utxos, height)
	case blockchain.NTBlockDisconnected:
		m.collateralCache.ClearMinerReuse(*block.MsgBlock().Utxos, height)
	}
}
