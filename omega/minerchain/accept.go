/* Copyright (C) 2019-2021 Omegasuite developers - All Rights Reserved
* This file is part of the omega chain library.
*
* Use of this source code is governed by license that can be
* found in the LICENSE file.
*
 */

package minerchain

import (
	"btcd/wire/common"
	"btcutil"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
	"omega/chainmap"
	"omega/runtimemetrics"
	"sort"

	//	"github.com/omegasuite/btcutil/base58"

	"btcd/blockchain"
	"btcd/blockchain/chainutil"
	"btcd/database"
	"btcd/wire"
	"github.com/omegasuite/btcd/btcec"
	"math/big"
)

// maybeAcceptBlock potentially accepts a block into the miner chain and, if
// accepted, returns whether or not it is on the main chain.  It performs
// several validation checks which depend on its position within the miner chain
// before adding it.  The block is expected to have already gone through
// ProcessBlock before calling this function with it.
//
// The flags are also passed to checkBlockContext and connectBestChain.  See
// their documentation for how the flags modify their behavior.
//
// This function MUST be called with the chain state lock held (for writes).
func (b *MinerChain) maybeAcceptBlock(block *wire.MinerBlock, flags blockchain.BehaviorFlags) (bool, error) {
	// The height of this block is one more than the referenced previous
	// block.
	prevHash := &block.MsgBlock().PrevBlock
	prevNode := b.index.LookupNode(prevHash)
	blockHeight := int32(0)
	if prevNode == nil {
		if !block.Hash().IsEqual(b.chainParams.GenesisMinerHash) {
			str := fmt.Sprintf("previous block %s is unknown", prevHash)
			return false, ruleError(ErrPreviousBlockUnknown, str)
		}
	} else if b.index.NodeStatus(prevNode).KnownInvalid() {
		str := fmt.Sprintf("previous block %s is known to be invalid", prevHash)
		return false, ruleError(ErrInvalidAncestorBlock, str)
	} else {
		blockHeight = prevNode.Height + 1
	}

	block.SetHeight(blockHeight)

	// The block must pass all of the validation rules which depend on the
	// position of the block within the block chain.
	if prevNode != nil {
		err := b.checkBlockContext(block, prevNode, flags)
		if err != nil && (flags&blockchain.BFEasyBlocks) == 0 {
			if _, ok := err.(RuleError); ok {
				return false, err
			}
			flags |= blockchain.BFNoReorg | blockchain.BFSideChain
		}

		sum := uint32(0)
		v2 := prevNode.Data.(*blockchainNodeData).block.MeanTPH
		for _, v := range block.MsgBlock().TphReports {
			if (v > v2*8 || 8*v < v2) && v2 > 0 {
				return false, ruleError(ErrInvalidAncestorBlock, "Out of range TPH score")
			}
			sum += v
		}
		if len(block.MsgBlock().TphReports) == 0 {
			sum = 1
		} else {
			sum /= uint32(len(block.MsgBlock().TphReports))
		}
		var meanTPH uint32
		meanTPH = (v2*63 + sum) >> 6

		if meanTPH == 0 {
			meanTPH = 1
		}
		if meanTPH != block.MsgBlock().MeanTPH {
			return false, ruleError(ErrInvalidAncestorBlock, "Incorrect mean TPH score")
		}
	}

	// Insert the block into the database if it's not already there.  Even
	// though it is possible the block will ultimately fail to connect, it
	// has already passed all proof-of-work and validity tests which means
	// it would be prohibitively expensive for an attacker to fill up the
	// disk with a bunch of blocks that fail to connect.  This is necessary
	// since it allows block download to be decoupled from the much more
	// expensive connection logic.  It also has some other nice properties
	// such as making blocks that never become part of the main chain or
	// blocks that fail to connect available for further analysis.
	err := b.db.Update(func(dbTx database.Tx) error {
		return dbStoreMinerBlock(dbTx, block)
	})
	if err != nil {
		return false, err
	}

	// Create a new block node for the block and add it to the node index. Even
	// if the block ultimately gets connected to the main chain, it starts out
	// on a side chain.
	blockHeader := block.MsgBlock()
	newNode := NewBlockNode(blockHeader, prevNode)
	newNode.Status = chainutil.StatusDataStored

	b.index.AddNode(newNode)
	err = b.index.FlushToDB(dbStoreBlockNode)
	if err != nil {
		return false, err
	}

	// Connect the passed block to the chain while respecting proper chain
	// selection according to the chain with the most proof of work.  This
	// also handles validation of the transaction scripts.
	isMainChain, err := b.connectBestChain(newNode, block, flags)
	if err != nil {
		log.Infof("connectBestChain failed. %s", err.Error())
		return false, err
	}

	//	log.Infof("isMainChain = %d", isMainChain)

	// Notify the caller that the new block was accepted into the block
	// chain.  The caller would typically want to react by relaying the
	// inventory to other peers.
	b.chainLock.Unlock()
	b.sendNotification(blockchain.NTBlockAccepted, block)
	b.chainLock.Lock()

	//	log.Infof("maybeAcceptBlock done")

	return isMainChain, nil
}

// dbStoreBlock stores the provided block in the database if it is not already
// there. The full block data is written to ffldb.
func dbStoreMinerBlock(dbTx database.Tx, block *wire.MinerBlock) error {
	h := block.Hash()
	hasBlock, err := dbTx.HasBlock(h)
	if err != nil {
		return err
	}
	if hasBlock {
		return nil
	}
	return dbTx.StoreMinerBlock(block)
}

// checkProofOfWork ensures the block header bits which indicate the target
// difficulty is in min/max range and that the block hash is less than the
// target difficulty as claimed.
//
// The flags modify the behavior of this function as follows:
//   - BFNoPoWCheck: The check to ensure the block hash is less than the target
//     difficulty is not performed.
func (m *MinerChain) checkProofOfWork(header *wire.MingingRightBlock, powLimit *big.Int, flags blockchain.BehaviorFlags) error {
	// The target difficulty must be larger than zero.
	target := CompactToBig(header.Bits)
	if target.Sign() <= 0 {
		str := fmt.Sprintf("MinerChain.checkProofOfWork: block target difficulty of %064x is too low", target)
		return ruleError(ErrUnexpectedDifficulty, str)
	}

	// The target difficulty must be less than the maximum allowed.
	if target.Cmp(powLimit) > 0 && flags&blockchain.BFEasyBlocks == 0 {
		str := fmt.Sprintf("MinerChain.checkProofOfWork: block target difficulty of %064x is "+
			"higher than max of %064x", target, powLimit)
		return ruleError(ErrUnexpectedDifficulty, str)
	}

	// The block hash must be less than the claimed target unless the flag
	// to avoid proof of work checks is set.
	if flags&blockchain.BFNoPoWCheck != blockchain.BFNoPoWCheck {
		// The block hash must be less than the claimed target.
		hash := header.BlockHash()
		hashNum := HashToBig(&hash)

		parentNode := m.index.LookupNode(&header.PrevBlock)
		if parentNode == nil {
			str := fmt.Sprintf("previous block %s is unknown for h1 denominator", &header.PrevBlock)
			return ruleError(ErrPreviousBlockUnknown, str)
		}

		factor := int64(1)
		h := uint32(parentNode.Height)
		if flags&blockchain.BFWatingFactor == blockchain.BFWatingFactor {
			factor = m.factorPOW(h, header.BestBlock)
		}
		//		if factor < 0 {
		//			return fmt.Errorf("Curable POW factor error.")
		//		}

		// since Ver 0x20000, the formula is:
		// 2 * hashNum * factor <= target * (h1 + h2)
		// h1 is collacteral factor, h2 is tps factor
		//			factor *= 16

		v, err := m.blockChain.CheckCollateral(wire.NewMinerBlock(header), &header.BestBlock, flags)
		if err != nil {
			return err
		}

		// PR-12: must be the only acceptH1Bindings call in checkProofOfWork.
		bindings, err := acceptH1Bindings(header, parentNode, m.chainParams)
		if err != nil {
			return ruleError(ErrUnexpectedDifficulty, err.Error())
		}
		c, err := bindings.Resolve()
		if err != nil {
			return ruleError(ErrUnexpectedDifficulty, err.Error())
		}

		h1 := int64(v / c)
		if h1 < 1 {
			h1 = 1
		}

		prev, _ := m.DBBlockByHash(&header.PrevBlock)
		minscore := prev.MsgBlock().MeanTPH >> 3
		if minscore == 0 {
			minscore = 1
		}

		r := m.TPSreportFromDB(header.Miner, h) // max most recent 100 records
		for i := len(r); i < 100; i++ {
			r = append(r, blockchain.TPSrv{Val: minscore})
		}
		sort.Slice(r, func(i, j int) bool {
			return r[i].Val < r[j].Val
		})

		sum := uint32(0)
		for k := 25; k < 75; k++ {
			sum += r[k].Val
		}
		sum /= 50

		h2 := int64(1)
		if sum <= minscore {
			h2 = 1
		} else {
			h2 = int64(sum / minscore)
		}

		if factor > 0 {
			hashNum = hashNum.Mul(hashNum, big.NewInt(factor))
			target = target.Mul(target, big.NewInt(h1+h2))
		} else {
			f := big.NewInt(-factor)
			target = target.Mul(target, f.Mul(f, big.NewInt(h1+h2)))
		}

		if target.Cmp(powLimit) > 0 {
			target = powLimit
		}

		if hashNum.Cmp(target) > 0 {
			str := fmt.Sprintf("block hash of %064x is higher than "+
				"expected max of %064x", hashNum, target)
			return ruleError(ErrHighHash, str)
		}
	}

	return nil
}

//func (m *MinerChain) factorPOW(firstNode *chainutil.BlockNode) int64 {
//baseh := uint32(firstNode.Height)
//best := firstNode.Data.(*blockchainNodeData).block.BestBlock

func (m *MinerChain) factorPOW(baseh uint32, best chainhash.Hash) int64 {
	var d = int32(0)
	var h = int32(0)

hit:
	for p := m.blockChain.NodeByHash(&best); p != nil; p = m.blockChain.ParentNode(p) {
		switch {
		case p.Data.GetNonce() > 0:
			d += wire.POWRotate

		case p.Data.GetNonce() <= -wire.MINER_RORATE_FREQ:
			h = -(p.Data.GetNonce() + wire.MINER_RORATE_FREQ)
			break hit
		}
	}

	h += d

	if h < 0 {
		return -1
	}

	d = int32(baseh) - h

	if d-wire.DESIRABLE_MINER_CANDIDATES > wire.SCALEFACTORCAP {
		return int64(1) << wire.SCALEFACTORCAP
	} else if d < wire.DESIRABLE_MINER_CANDIDATES {
		m := wire.DESIRABLE_MINER_CANDIDATES - d
		if m > wire.SCALEFACTORCAP {
			m = wire.SCALEFACTORCAP
		}
		return (-1) << m
	}

	return int64(1) << (d - wire.DESIRABLE_MINER_CANDIDATES)
}

func (b *MinerChain) ValidateOps(block *wire.MinerBlock) error {
	blk := block.MsgBlock()
	if len(blk.Instructions) > 255 {
		return fmt.Errorf("Too many ops in MR block")
	}

	for _, op := range blk.Instructions {
		switch op.InstCode {
		case wire.AddDns:
			if len(op.InstData) == 0 { // UTXO of asset to withdraw
				return fmt.Errorf("Incorrect op data")
			}
			ac := chainmap.ChainDescriptor{
				Version: 0x10000,
				Legacy:  false,
				Final:   21,
			}
			err := json.Unmarshal(op.InstData, &ac)
			if _, ok := chainmap.AllChains[b.chainParams.ChainID]; !ok {
				return fmt.Errorf("Incorrect op data")
			}
			if chainmap.AllChains[b.chainParams.ChainID].ChainMap[ac.ChainID].Magic != ac.Magic {
				return fmt.Errorf("Incorrect op data")
			}
			if ac.Dns == "" {
				return fmt.Errorf("Incorrect op data")
			}
			// TBD: check IP
			if err != nil {
				return err
			}

		case wire.AddChain:
			if len(op.InstData) == 0 { // UTXO of asset to withdraw
				return fmt.Errorf("Incorrect op data")
			}
			ac := chainmap.ChainDescriptor{
				Version: 0x10000,
				Legacy:  false,
				Final:   21,
			}
			err := json.Unmarshal(op.InstData, &ac)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// checkBlockContext peforms several validation checks on the block which depend
// on its position within the block chain.
//
// The flags modify the behavior of this function as follows:
//   - BFFastAdd: The transaction are not checked to see if they are finalized
//     and the somewhat expensive BIP0034 validation is not performed.
//
// The flags are also passed to checkBlockHeaderContext.  See its documentation
// for how the flags modify its behavior.
//
// This function MUST be called with the chain state lock held (for writes).
func (b *MinerChain) checkBlockContext(block *wire.MinerBlock, prevNode *chainutil.BlockNode, flags blockchain.BehaviorFlags) error {
	fastAdd := flags&blockchain.BFFastAdd == blockchain.BFFastAdd

	if fastAdd {
		return nil
	}

	header := block.MsgBlock()

	for p, i := prevNode, 0; p != nil && i < wire.MinerGap; i++ {
		h := NodetoHeader(p)
		if len(block.MsgBlock().Connection) > 0 && len(h.Connection) > 0 && bytes.Compare(h.Connection, block.MsgBlock().Connection) == 0 {
			str := "Miner's IP/port has appeared in the past %d blocks"
			str = fmt.Sprintf(str, wire.MinerGap)
			runtimemetrics.IncIPMinerGapReject()
			return ruleError(ErrRotationViolation, str)
		}
		if bytes.Compare(h.Miner[:], block.MsgBlock().Miner[:]) == 0 {
			str := "Miner has appeared in the past %d blocks"
			str = fmt.Sprintf(str, wire.MinerGap)
			runtimemetrics.IncAddrMinerGapReject()
			return ruleError(ErrRotationViolation, str)
		}
		p = p.Parent
	}

	// Ensure the difficulty specified in the block header matches
	// the calculated difficulty based on the previous block and
	// difficulty retarget rules.
	if !b.IsSVP {
		expectedDifficulty, coll, _ := b.calcNextRequiredDifficulty(prevNode, header.Timestamp)

		blockDifficulty := header.Bits
		if blockDifficulty != expectedDifficulty {
			str := "block difficulty of %d is not the expected value of %d"
			str = fmt.Sprintf(str, blockDifficulty, expectedDifficulty)
			return ruleError(ErrUnexpectedDifficulty, str)
		}

		if header.Collateral != coll {
			str := "block collateral of %d is not the expected value of %d"
			str = fmt.Sprintf(str, header.Collateral, coll)
			return ruleError(ErrUnexpectedDifficulty, str)
		}

		// Ensure the timestamp for the block header is after the
		// median time of the last several blocks (medianTimeBlocks).
		medianTime := prevNode.CalcPastMedianTime()
		if !header.Timestamp.After(medianTime) {
			str := "block timestamp of %v is not after expected %v"
			str = fmt.Sprintf(str, header.Timestamp, medianTime)
			return ruleError(ErrTimeTooOld, str)
		}
	}

	// the following condition must be met before NewNodeBlock may be accepted
	// hash of: PrevBlock + ReferredBlock + BestBlock + Newnode + Nonce must be within Bits Difficulty target, which is
	// set periodically according to NewNodeBlock chain data. The target is to set based on the number of miner
	// candidates as decided by the height of NewNodeBlock chain and the height of NewNodeBlock referred by
	// latest committee in main chain upto ReferredBlock. If this is below MINER_RORATE_FREQ, the difficulty
	// is set to generate 2 NewNodeBlock every MINER_RORATE_FREQ block time. Once number of miner candidates reaches
	// MINER_RORATE_FREQ, the difficulty increases 20% for every one more candidate.

	xf := blockchain.BFWatingFactor
	if b.chainParams.Net == uint32(common.TestNet) || b.chainParams.Net == uint32(common.SimNet) || b.chainParams.Net == uint32(common.RegNet) {
		xf |= blockchain.BFEasyBlocks
	}

	if !b.IsSVP {
		if err := b.checkProofOfWork(header, b.chainParams.PowLimit, flags|xf); err != nil {
			return err
		}
	}

	// validity of Violations
	uniq := make(map[chainhash.Hash]map[chainhash.Hash]struct{})
	mh, _ := b.blockChain.BlockHeightByHash(&block.MsgBlock().BestBlock)
	if !b.IsSVP {
		for _, p := range block.MsgBlock().ViolationReport {
			if p.Height <= 0 {
				return ruleError(ErrBlackList, fmt.Errorf("Invalid height: %d", p.Height).Error())
			}
			if p.Height > mh {
				// If side chain is higher, that means we should choose it as best chain
				return ruleError(ErrBlackList, fmt.Errorf("Invalid height: %d", p.Height).Error())
			}
			if len(p.Blocks) < 2 {
				return ruleError(ErrBlackList, fmt.Errorf("Invalid evidence: %d blocks", len(p.Blocks)).Error())
			}
			if !b.BestChain.Contains(b.NodeByHash(&p.MRBlock)) {
				return ruleError(ErrBlackList, fmt.Errorf("Invalid evidence: block %s not in MR chain", p.MRBlock.String()).Error())
			}
			mb, _ := b.BlockByHash(&p.MRBlock)
			if mb.Height() < block.Height()-99 {
				return ruleError(ErrBlackList, fmt.Errorf("Report of violation more than 99 blocks older not allowed. %d", mb.Height()).Error())
			}
			miner := mb.MsgBlock().Miner

			if _, ok := uniq[p.MRBlock]; !ok {
				uniq[p.MRBlock] = make(map[chainhash.Hash]struct{})
			}

			// prep for check for duplicated reports
			for q, _ := b.BlockByHash(&block.MsgBlock().PrevBlock); q.Height() > mb.Height(); q, _ = b.BlockByHash(&q.MsgBlock().PrevBlock) {
				for _, s := range q.MsgBlock().ViolationReport {
					if !s.MRBlock.IsEqual(&p.MRBlock) {
						continue
					}
					if _, ok := uniq[s.MRBlock]; !ok {
						uniq[s.MRBlock] = make(map[chainhash.Hash]struct{})
					}
					for _, tx := range s.Blocks {
						if _, err := b.blockChain.BlockHeightByHash(&tx); err != nil {
							uniq[s.MRBlock][tx] = struct{}{}
						}
					}
				}
			}

			main := false
			for _, tx := range p.Blocks {
				if _, err := b.blockChain.BlockHeightByHash(&tx); err != nil {
					if _, ok := uniq[p.MRBlock][tx]; ok {
						return ruleError(ErrBlackList, fmt.Errorf("Violating block already reported before: %s", tx.String()).Error())
					}
					uniq[p.MRBlock][tx] = struct{}{}
				} else if main {
					return ruleError(ErrBlackList, fmt.Errorf("Duplicated block in blacklist: %s", tx.String()).Error())
				} else {
					main = true
				}
				tb, _ := b.blockChain.HashToBlock(&tx) // already checked that it exists
				if tb == nil || tb.Height() != p.Height {
					return ruleError(ErrBlackList, fmt.Errorf("Invalid height of violating block: %s", tx.String()).Error())
				}

				mtch := false
				for _, sig := range tb.MsgBlock().Transactions[0].SignatureScripts[1:] {
					// although tb is in side chain, the fact that it is the database means
					// that all block signatures has been verified. thus we don't need to
					// verify signature again. we only need to extract address from pub key
					h := btcutil.Hash160(sig[:btcec.PubKeyBytesLenCompressed])
					if bytes.Compare(h, miner[:]) == 0 {
						mtch = true
						break
					}
				}
				if !mtch {
					return ruleError(ErrBlackList, fmt.Errorf("Invalid report: %s", tx.String()).Error())
				}
			}
		}
	}

	nextBlockVersion, err := b.NextBlockVersion(prevNode, false)
	if err != nil || (header.Version&0xFFFF0000) < (nextBlockVersion&0xFFFF0000) ||
		(header.Version > nextBlockVersion && header.Version > wire.CodeVersion) {
		//		(header.Version & 0xFFFF0000) > ((nextBlockVersion + 0xFFFF) & 0xFFFF0000) ||
		//		(header.Version > nextBlockVersion && (header.Version & 0xFFFF0000) == (nextBlockVersion & 0xFFFF0000)){
		// fail if: 1. major version is less than expected
		// 2. version is larger than expected
		return fmt.Errorf("Incorrect block version")
	}

	// validate opes
	err = b.ValidateOps(block)
	if err != nil {
		return err
	}

	return nil
}

// CheckConnectBlockTemplate fully validates that connecting the passed block to
// the main chain does not violate any consensus rules, aside from the proof of
// work requirement. The block must connect to the current tip of the main chain.
//
// This function is safe for concurrent access.
func (b *MinerChain) CheckConnectBlockTemplate(block *wire.MinerBlock) error {
	//	log.Infof("MinerChain.CheckConnectBlockTemplate: ChainLock.RLock")
	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	// Skip the proof of work check as this is just a block template.
	flags := blockchain.BFNoPoWCheck
	if b.chainParams.Net == uint32(common.TestNet) || b.chainParams.Net == uint32(common.SimNet) || b.chainParams.Net == uint32(common.RegNet) {
		flags |= blockchain.BFEasyBlocks
	}
	tip := b.BestChain.Tip()

	/*
		// This only checks whether the block can be connected to the tip of the
		// current chain.
		header := block.MsgBlock()

		if tip.Hash != header.PrevBlock {
			str := fmt.Sprintf("previous block must be the current chain tip %v, "+
				"instead got %v", tip.Hash, header.PrevBlock)
			return ruleError(ErrPrevBlockNotBest, str)
		}
	*/

	err := CheckBlockSanity(block, b.chainParams.PowLimit, b.timeSource, flags)
	if err != nil {
		return err
	}

	return b.checkBlockContext(block, tip, flags)
}
