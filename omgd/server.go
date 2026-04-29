// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2018 The Decred develserver.dbopers
// Copyright (C) 2019-2021 Omegasuite developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

// import "C"

import (
	//	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"

	//	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"

	"btcd/blockchain/chainutil"
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/omegasuite/btcd/btcec"
	"omega/minerchain"
	//	"io"
	"math"
	"net"
	//	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"btcd/addrmgr"
	"btcd/blockchain"
	"btcd/blockchain/indexers"
	"btcd/chaincfg"
	"btcd/connmgr"
	"btcd/database"
	"btcd/mempool"
	"btcd/mining"
	"btcd/mining/cpuminer"
	"btcd/netsync"
	"btcd/peer"
	"btcd/wire"
	"btcd/wire/common"
	"btcutil"
	"btcutil/bloom"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
	"omega/chainmap"
	"omega/viewpoint"
)

const (
	// defaultServices describes the default services that are supported by
	// the server.
	defaultServices = common.SFNodeNetwork | common.SFNodeBloom | common.SFNodeCF

	// defaultRequiredServices describes the default services that are
	// required to be supported by outbound peers.
	defaultRequiredServices = common.SFNodeNetwork

	// defaultTargetOutbound is the default number of outbound peers to target.
	defaultTargetOutbound = 8

	// connectionRetryInterval is the base amount of time to wait in between
	// retries when connecting to persistent peers.  It is adjusted by the
	// number of retries such that there is a retry backoff.
	connectionRetryInterval = time.Second * 5
)

var (
	// userAgentName is the user agent name and is used to help identify
	// ourselves to other bitcoin peers.
	userAgentName = "omgd"

	// userAgentVersion is the user agent version and is used to help
	// identify ourselves to other bitcoin peers.
	userAgentVersion = fmt.Sprintf("%d.%d.%d", appMajor, appMinor, appPatch)
)

// zeroHash is the zero value hash (all zeros).  It is defined as a convenience.
var zeroHash chainhash.Hash

// onionAddr implements the net.Addr interface and represents a tor address.
type onionAddr struct {
	addr string
}

// String returns the onion address.
//
// This is part of the net.Addr interface.
func (oa *onionAddr) String() string {
	return oa.addr
}

// Network returns "onion".
//
// This is part of the net.Addr interface.
func (oa *onionAddr) Network() string {
	return "onion"
}

// Ensure onionAddr implements the net.Addr interface.
var _ net.Addr = (*onionAddr)(nil)

// simpleAddr implements the net.Addr interface with two struct fields
type simpleAddr struct {
	net, addr string
}

// String returns the address.
//
// This is part of the net.Addr interface.
func (a simpleAddr) String() string {
	return a.addr
}

// Network returns the network.
//
// This is part of the net.Addr interface.
func (a simpleAddr) Network() string {
	return a.net
}

// Ensure simpleAddr implements the net.Addr interface.
var _ net.Addr = simpleAddr{}

// broadcastMsg provides the ability to house a bitcoin message to be broadcast
// to all connected peers except specified excluded peers.
type broadcastMsg struct {
	message      wire.Message
	excludePeers []*serverPeer
}

// broadcastInventoryAdd is a type used to declare that the InvVect it contains
// needs to be added to the rebroadcast map
type broadcastInventoryAdd relayMsg

// broadcastInventoryDel is a type used to declare that the InvVect it contains
// needs to be removed from the rebroadcast map
type broadcastInventoryDel *wire.InvVect

// relayMsg packages an inventory vector along with the newly discovered
// inventory so the relay has access to that information.
type relayMsg struct {
	invVect *wire.InvVect
	data    interface{}
}

// updatePeerHeightsMsg is a message sent from the blockmanager to the server
// after a new block has been accepted. The purpose of the message is to update
// the heights of peers that were known to announce the block before we
// connected it to the main chain or recognized it as an orphan. With these
// updates, peer heights will be kept up to date, allowing for fresh data when
// selecting sync peer candidacy.
type updatePeerHeightsMsg struct {
	newHash    *chainhash.Hash
	newHeight  int32
	originPeer *peer.Peer
}

type msgnb struct {
	msg  wire.Message
	done chan bool
}

type committeeState struct {
	peers       []*serverPeer
	member      [20]byte
	queue       chan msgnb
	closed      bool
	address     string
	minerHeight int32
	connecting  bool
	retry       uint32
	msgsent     uint32
}

// peerState maintains state of inbound, persistent, outbound peers as well
// as banned peers and outbound groups.
type peerState struct {
	connManager     *connmgr.ConnManager
	inboundPeers    map[int32]*serverPeer
	outboundPeers   map[int32]*serverPeer
	persistentPeers map[int32]*serverPeer
	banned          map[string]time.Time
	outboundGroups  map[string]int

	// committee members.
	cmutex    sync.Mutex
	qmutex    sync.Mutex
	committee map[[20]byte]*committeeState
}

func (p *peerState) NewCommitteeState(m [20]byte, h int32, addr string) *committeeState {
	adr := ""
	if len(addr) > 0 {
		tcp, err := net.ResolveTCPAddr("", addr)
		adr = tcp.String()
		if err != nil {
			adr = addr
		}
	}

	t := &committeeState{
		peers:       make([]*serverPeer, 0),
		queue:       make(chan msgnb, 50),
		closed:      false,
		member:      m,
		minerHeight: h,
		address:     adr,
		//		connecting:  false,
		retry: 0,
	}

	go p.CommitteeOut(t)

	return t
}

func (state *peerState) RemovePeer(sp *serverPeer) {
	for _, m := range state.committee {
		for todel := true; todel; {
			todel = false
			for i, p := range m.peers {
				if p.ID() == sp.ID() {
					if len(m.peers) == 1 {
						m.peers = m.peers[:0]
					} else if i == len(m.peers)-1 {
						m.peers = m.peers[:i]
					} else if i == 0 {
						m.peers = m.peers[1:]
					} else {
						m.peers = append(m.peers[:i], m.peers[i+1:]...)
					}
					todel = true
					break
				}
			}
		}
	}
}

func (p *peerState) ConCount() int {
	return len(p.inboundPeers) + len(p.outboundPeers) + len(p.persistentPeers)
}

func (p *peerState) IsConnected(c *connmgr.ConnReq) bool {
	iscontd := false
	p.ForAllOutboundPeers(func(sp *serverPeer) {
		if !iscontd && c.Addr.String() == sp.connReq.Addr.String() &&
			(sp.Connected() || sp.persistent) {
			iscontd = true
		}
	})

	return iscontd
}

// Count returns the count of all known peers.
func (ps *peerState) Count(m byte) int {
	s := 0
	if m&1 != 0 {
		s += len(ps.inboundPeers)
	}
	if m&2 != 0 {
		s += len(ps.outboundPeers)
	}
	if m&4 != 0 {
		s += len(ps.persistentPeers)
	}
	return s
}

// ForAllOutboundPeers is a helper function that runs closure on all outbound
// peers known to peerState.
func (ps *peerState) ForAllOutboundPeers(closure func(sp *serverPeer)) {
	ps.cmutex.Lock()

	ps.forAllOutboundPeers(closure)

	ps.cmutex.Unlock()
}

func (ps *peerState) forAllOutboundPeers(closure func(sp *serverPeer)) {
	for _, e := range ps.outboundPeers {
		closure(e)
	}
	for _, e := range ps.persistentPeers {
		closure(e)
	}
}

// ForAllPeers is a helper function that runs closure on all peers known to
// peerState.
func (ps *peerState) ForAllPeers(closure func(sp *serverPeer)) {
	ps.cmutex.Lock()
	ps.forAllPeers(closure)

	ps.cmutex.Unlock()
}

func (ps *peerState) forAllPeers(closure func(sp *serverPeer)) {
	for _, e := range ps.inboundPeers {
		closure(e)
	}
	ps.forAllOutboundPeers(closure)
}

// cfHeaderKV is a tuple of a filter header and its associated block hash. The
// struct is used to cache cfcheckpt responses.
type cfHeaderKV struct {
	blockHash    chainhash.Hash
	filterHeader chainhash.Hash
}

// server provides a bitcoin server for handling communications to and from
// bitcoin peers.
type server struct {
	// The following variables must only be used atomically.
	// Putting the uint64s first makes them 64-bit aligned for 32-bit systems.
	bytesReceived uint64 // Total bytes received from all peers since start.
	bytesSent     uint64 // Total bytes sent by all peers since start.
	started       int32
	shutdown      int32
	shutdownSched int32
	startupTime   int64

	chainParams *chaincfg.Params
	addrManager *addrmgr.AddrManager
	connManager *connmgr.ConnManager

	rpcServer              *rpcServer
	syncManager            *netsync.SyncManager
	chain                  *blockchain.BlockChain
	txMemPool              *mempool.TxPool
	cpuMiner               *cpuminer.CPUMiner
	minerMiner             *minerchain.CPUMiner
	modifyRebroadcastInv   chan interface{}
	newPeers               chan *serverPeer
	donePeers              chan *serverPeer
	banPeers               chan *serverPeer
	query                  chan interface{}
	relayInv               chan relayMsg
	broadcast              chan broadcastMsg
	peerHeightsUpdate      chan updatePeerHeightsMsg
	peerMinerHeightsUpdate chan updatePeerHeightsMsg
	wg                     sync.WaitGroup
	quit                   chan struct{}
	nat                    NAT
	db                     database.DB
	minerdb                database.DB
	timeSource             chainutil.MedianTimeSource
	services               common.ServiceFlag

	// The following fields are used for optional indexes.  They will be nil
	// if the associated index is not enabled.  These fields are set during
	// initial creation of the server and never changed afterwards, so they
	// do not need to be protected for concurrent access.
	txIndex      *indexers.TxIndex
	addrIndex    *indexers.AddrIndex
	addrUseIndex *indexers.AddrUseIndex

	// The fee estimator keeps track of how long transactions are left in
	// the mempool before they are mined into blocks.
	feeEstimator *mempool.FeeEstimator

	// cfCheckptCaches stores a cached slice of filter headers for cfcheckpt
	// messages for each filter type.
	cfCheckptCaches    map[wire.FilterType][]cfHeaderKV
	cfCheckptCachesMtx sync.RWMutex
	signAddress        []btcutil.Address
	privKeys           []*btcec.PrivateKey
	peerState          *peerState

	// broadcasted is the inventory of message we have broadcasted,
	// the purpose is to prevent rebroadcast
	Broadcasted map[chainhash.Hash]int64
	alerted     map[int32]struct{}

	// protocol data
	prot *Protocol
}

// serverPeer extends the peer to maintain state shared by the server and
// the blockmanager.
type serverPeer struct {
	// The following variables must only be used atomically
	feeFilter int64

	*peer.Peer

	connReq           *connmgr.ConnReq
	server            *server
	persistent        bool
	continueHash      *chainhash.Hash
	continueMinerHash *chainhash.Hash
	hashStop          chainhash.Hash
	minerHashStop     chainhash.Hash

	//	heightSent     [2]int32			// heights (tx, miner) of the best mainchain block send
	relayMtx       sync.Mutex
	disableRelayTx bool
	sentAddrs      bool
	isWhitelisted  bool
	filter         *bloom.Filter
	knownAddresses map[string]struct{}
	banScore       connmgr.DynamicBanScore
	quit           chan struct{}
	// The following chans are used to sync blockmanager and server.
	txProcessed    chan struct{}
	blockProcessed chan struct{}
}

// newServerPeer returns a new serverPeer instance. The peer needs to be set by
// the caller.
func newServerPeer(s *server, isPersistent bool) *serverPeer {
	return &serverPeer{
		server:         s,
		persistent:     isPersistent,
		filter:         bloom.LoadFilter(nil),
		knownAddresses: make(map[string]struct{}),
		quit:           make(chan struct{}),
		txProcessed:    make(chan struct{}, 1),
		blockProcessed: make(chan struct{}, 1),
	}
}

// newestBlock returns the current best block hash and height using the format
// required by the configuration for the peer package.
func (sp *serverPeer) newestBlock() (*chainhash.Hash, int32, error) {
	if sp.server.chain == nil {
		return &chainhash.Hash{}, 0, nil
	}
	best := sp.server.chain.BestSnapshot()
	return &best.Hash, best.Height, nil
}

func (sp *serverPeer) newestMinerBlock() (*chainhash.Hash, int32, error) {
	if sp.server.chain == nil {
		return &chainhash.Hash{}, 0, nil
	}
	best := sp.server.chain.Miners.BestSnapshot()
	return &best.Hash, best.Height, nil
}

// addKnownAddresses adds the given addresses to the set of known addresses to
// the peer to prevent sending duplicate addresses.
func (sp *serverPeer) addKnownAddresses(addresses []*wire.NetAddress) {
	for _, na := range addresses {
		sp.knownAddresses[addrmgr.NetAddressKey(na)] = struct{}{}
	}
}

// addressKnown true if the given address is already known to the peer.
func (sp *serverPeer) addressKnown(na *wire.NetAddress) bool {
	_, exists := sp.knownAddresses[addrmgr.NetAddressKey(na)]
	return exists
}

// setDisableRelayTx toggles relaying of transactions for the given peer.
// It is safe for concurrent access.
func (sp *serverPeer) setDisableRelayTx(disable bool) {
	sp.relayMtx.Lock()
	sp.disableRelayTx = disable
	sp.relayMtx.Unlock()
}

// relayTxDisabled returns whether or not relaying of transactions for the given
// peer is disabled.
// It is safe for concurrent access.
func (sp *serverPeer) relayTxDisabled() bool {
	sp.relayMtx.Lock()
	isDisabled := sp.disableRelayTx
	sp.relayMtx.Unlock()

	return isDisabled
}

// pushAddrMsg sends an addr message to the connected peer using the provided
// addresses.
func (sp *serverPeer) pushAddrMsg(addresses []*wire.NetAddress) {
	// Filter addresses already known to the peer.
	addrs := make([]*wire.NetAddress, 0, len(addresses))
	for _, addr := range addresses {
		if !sp.addressKnown(addr) {
			addrs = append(addrs, addr)
		}
	}
	known, err := sp.PushAddrMsg(addrs)
	if err != nil {
		peerLog.Errorf("Can't push address message to %s: %v", sp.Peer, err)
		sp.Disconnect("pushAddrMsg")
		return
	}
	sp.addKnownAddresses(known)
}

// addBanScore increases the persistent and decaying ban score fields by the
// values passed as parameters. If the resulting score exceeds half of the ban
// threshold, a warning is logged including the reason provided. Further, if
// the score is above the ban threshold, the peer will be banned and
// disconnected.
func (sp *serverPeer) addBanScore(persistent, transient uint32, reason string) {
	// No warning is logged and no score is calculated if banning is disabled.
	if sp.server.prot.cfg.DisableBanning {
		return
	}
	if sp.isWhitelisted {
		peerLog.Debugf("Misbehaving whitelisted peer %s: %s", sp, reason)
		return
	}

	warnThreshold := sp.server.prot.cfg.BanThreshold >> 1
	if transient == 0 && persistent == 0 {
		// The score is not being increased, but a warning message is still
		// logged if the score is above the warn threshold.
		score := sp.banScore.Int()
		if score > warnThreshold {
			peerLog.Warnf("Misbehaving peer %s: %s -- ban score is %d, "+
				"it was not increased this time", sp, reason, score)
		}
		return
	}
	score := sp.banScore.Increase(persistent, transient)
	if score > warnThreshold {
		peerLog.Warnf("Misbehaving peer %s: %s -- ban score increased to %d",
			sp, reason, score)
		if score > sp.server.prot.cfg.BanThreshold {
			if sp.Peer.Committee > 0 {
				return
			}
			peerLog.Warnf("Misbehaving peer %s -- banning and disconnecting",
				sp)
			sp.server.BanPeer(sp)
			sp.Disconnect("addBanScore")
		}
	}
}

// hasServices returns whether or not the provided advertised service flags have
// all of the provided desired service flags set.
func hasServices(advertised, desired common.ServiceFlag) bool {
	return advertised&desired == desired
}

// OnVersion is invoked when a peer receives a version bitcoin message
// and is used to negotiate the protocol version details as well as kick start
// the communications.
func (sp *serverPeer) OnVersion(_ *peer.Peer, msg *wire.MsgVersion) *wire.MsgReject {
	// Update the address manager with the advertised services for outbound
	// connections in case they have changed.  This is not done for inbound
	// connections to help prevent malicious behavior and is skipped when
	// running on the simulation test network since it is only intended to
	// connect to specified peers and actively avoids advertising and
	// connecting to discovered peers.
	//
	// NOTE: This is done before rejecting peers that are too old to ensure
	// it is updated regardless in the case a new minimum protocol version is
	// enforced and the remote node has not upgraded yet.
	isInbound := sp.Inbound()
	remoteAddr := sp.NA()
	addrManager := sp.server.addrManager
	if !sp.server.prot.cfg.SimNet && !isInbound {
		addrManager.SetServices(remoteAddr, msg.Services)
	}

	// Ignore peers that have a protcol version that is too old.  The peer
	// negotiation logic will disconnect it after this callback returns.
	if msg.ProtocolVersion < int32(peer.MinAcceptableProtocolVersion) {
		return nil
	}

	// Reject outbound peers that are not full nodes.
	wantServices := common.SFNodeNetwork
	if !isInbound && !hasServices(msg.Services, wantServices) {
		missingServices := wantServices & ^msg.Services
		srvrLog.Debugf("Rejecting peer %s with services %v due to not "+
			"providing desired services %v", sp.Peer, msg.Services,
			missingServices)
		reason := fmt.Sprintf("required services %#x not offered",
			uint64(missingServices))
		return wire.NewMsgReject(msg.Command(), common.RejectNonstandard, reason)
	}

	// Update the address manager and request known addresses from the
	// remote peer for outbound connections.  This is skipped when running
	// on the simulation test network since it is only intended to connect
	// to specified peers and actively avoids advertising and connecting to
	// discovered peers.
	if !sp.server.prot.cfg.SimNet && !isInbound {
		// After soft-fork activation, only make outbound
		// connection to peers if they flag that they're segwit
		// enabled.

		if !sp.IsWitnessEnabled() {
			peerLog.Infof("Disconnecting non-segwit peer %v, isn't segwit "+
				"enabled and we need more segwit enabled peers", sp)
			sp.Disconnect("OnVersion")
			return nil
		}

		// Advertise the local address when the server accepts incoming
		// connections and it believes itself to be close to the best known tip.
		if !sp.server.prot.cfg.DisableListen && sp.server.syncManager.IsCurrent() {
			// Get address that best matches.
			lna := addrManager.GetBestLocalAddress(remoteAddr)
			if addrmgr.IsRoutable(lna) {
				// Filter addresses the peer already knows about.
				addresses := []*wire.NetAddress{lna}
				sp.pushAddrMsg(addresses)
			}
		}

		// Request known addresses if the server address manager needs
		// more and the peer has a protocol version new enough to
		// include a timestamp with addresses.
		hasTimestamp := sp.ProtocolVersion() >= wire.NetAddressTimeVersion
		if addrManager.NeedMoreAddresses() && hasTimestamp {
			sp.QueueMessage(wire.NewMsgGetAddr(), nil)
		}

		// Mark the address as a known good address.
		addrManager.Good(remoteAddr)
	}

	// Add the remote peer time as a sample for creating an offset against
	// the local clock to keep the network time in sync.

	//	btcdLog.Infof("Msg time stamp is: %d and local time is %d", msg.Timestamp.Unix(), time.Now().Unix())
	sp.server.timeSource.AddTimeSample(sp.Addr(), msg.Timestamp)

	// Signal the sync manager this peer is a new sync candidate.
	sp.server.syncManager.NewPeer(sp.Peer)

	// Choose whether or not to relay transactions before a filter command
	// is received.
	sp.setDisableRelayTx(msg.DisableRelayTx)

	// Add valid peer to the server.
	sp.server.AddPeer(sp)
	return nil
}

// OnMemPool is invoked when a peer receives a mempool bitcoin message.
// It creates and sends an inventory message with the contents of the memory
// pool up to the maximum inventory allowed per message.  When the peer has a
// bloom filter loaded, the contents are filtered accordingly.
func (sp *serverPeer) OnMemPool(_ *peer.Peer, msg *wire.MsgMemPool) {
	// Only allow mempool requests if the server has bloom filtering
	// enabled.
	if sp.server.services&common.SFNodeBloom != common.SFNodeBloom {
		peerLog.Debugf("peer %v sent mempool request with bloom "+
			"filtering disabled -- disconnecting", sp)
		sp.Disconnect("OnMemPool")
		return
	}

	// A decaying ban score increase is applied to prevent flooding.
	// The ban score accumulates and passes the ban threshold if a burst of
	// mempool messages comes from a peer. The score decays each minute to
	// half of its value.
	sp.addBanScore(0, 33, "mempool")

	// Generate inventory message with the available transactions in the
	// transaction memory pool.  Limit it to the max allowed inventory
	// per message.  The NewMsgInvSizeHint function automatically limits
	// the passed hint to the maximum allowed, so it's safe to pass it
	// without double checking it here.
	txMemPool := sp.server.txMemPool
	txDescs := txMemPool.TxDescs()
	invMsg := wire.NewMsgInvSizeHint(uint(len(txDescs)))

	for _, txDesc := range txDescs {
		// Either add all transactions when there is no bloom filter,
		// or only the transactions that match the filter when there is
		// one.
		if !sp.filter.IsLoaded() || sp.filter.MatchTxAndUpdate(txDesc.Tx) {
			iv := wire.NewInvVect(common.InvTypeTx, txDesc.Tx.Hash())
			invMsg.AddInvVect(iv)
			if len(invMsg.InvList)+1 > wire.MaxInvPerMsg {
				break
			}
		}
	}

	// Send the inventory message if there is anything to send.
	if len(invMsg.InvList) > 0 {
		sp.QueueMessage(invMsg, nil)
	}
}

// OnTx is invoked when a peer receives a tx bitcoin message.  It blocks
// until the bitcoin transaction has been fully processed.  Unlock the block
// handler this does not serialize all transactions through a single thread
// transactions don't rely on the previous one in a linear fashion like blocks.
func (sp *serverPeer) OnTx(_ *peer.Peer, msg *wire.MsgTx) {
	if sp.server.prot.cfg.BlocksOnly {
		peerLog.Tracef("Ignoring tx %v from %v - blocksonly enabled",
			msg.TxHash(), sp)
		return
	}

	// Add the transaction to the known inventory for the peer.
	// Convert the raw MsgTx to a btcutil.Tx which provides some convenience
	// methods and things such as hash caching.
	tx := btcutil.NewTx(msg)
	iv := wire.NewInvVect(common.InvTypeTx, tx.Hash())
	sp.AddKnownInventory(iv)

	// Queue the transaction up to be handled by the sync manager and
	// intentionally block further receives until the transaction is fully
	// processed and known good or bad.  This helps prevent a malicious peer
	// from queuing up a bunch of bad transactions before disconnecting (or
	// being disconnected) and wasting memory.
	sp.server.syncManager.QueueTx(tx, sp.Peer, sp.txProcessed)
	<-sp.txProcessed
}

func (sp *serverPeer) OnSignatures(_ *peer.Peer, msg *wire.MsgSignatures) {
	sp.server.syncManager.QueueSignatures(msg, sp.Peer, sp.blockProcessed)
	<-sp.blockProcessed
}

// OnBlock is invoked when a peer receives a block bitcoin message.  It
// blocks until the bitcoin block has been fully processed.
func (sp *serverPeer) OnBlock(_ *peer.Peer, msg *wire.MsgBlock, buf []byte) {
	// Convert the raw MsgBlock to a btcutil.Block which provides some
	// convenience methods and things such as hash caching.
	block := btcutil.NewBlockFromBlockAndBytes(msg, buf)

	// Add the block to the known inventory for the peer.
	iv := wire.NewInvVect(common.InvTypeBlock, block.Hash())
	sp.AddKnownInventory(iv)

	// Queue the block up to be handled by the block
	// manager and intentionally block further receives
	// until the bitcoin block is fully processed and known
	// good or bad.  This helps prevent a malicious peer
	// from queuing up a bunch of bad blocks before
	// disconnecting (or being disconnected) and wasting
	// memory.  Additionally, this behavior is depended on
	// by at least the block acceptance test tool as the
	// reference implementation processes blocks in the same
	// thread and therefore blocks further messages until
	// the bitcoin block has been fully processed.

	//	btcdLog.Infof("Blocks %s received", block.Hash().String())

	sp.server.syncManager.QueueBlock(block, sp.Peer, sp.blockProcessed)

	// TBD. Take a return indicating whether the block has been added (orphan incld.)
	// if not, remove from known inventory
	<-sp.blockProcessed
}

// OnBlock is invoked when a peer receives a block bitcoin message.  It
// blocks until the bitcoin block has been fully processed.
func (sp *serverPeer) OnMinerBlock(_ *peer.Peer, msg *wire.MingingRightBlock, buf []byte) {
	// Convert the raw MsgBlock to a btcutil.Block which provides some
	// convenience methods and things such as hash caching.
	block := wire.NewMinerBlockFromBlockAndBytes(msg, buf)

	// Add the block to the known inventory for the peer.
	iv := wire.NewInvVect(common.InvTypeMinerBlock, block.Hash())
	sp.AddKnownInventory(iv)

	// Queue the block up to be handled by the block
	// manager and intentionally block further receives
	// until the bitcoin block is fully processed and known
	// good or bad.  This helps prevent a malicious peer
	// from queuing up a bunch of bad blocks before
	// disconnecting (or being disconnected) and wasting
	// memory.  Additionally, this behavior is depended on
	// by at least the block acceptance test tool as the
	// reference implementation processes blocks in the same
	// thread and therefore blocks further messages until
	// the bitcoin block has been fully processed.
	sp.server.syncManager.QueueMinerBlock(block, sp.Peer, sp.blockProcessed)

	// TBD. Take a return indicating whether the block has been added (orphan incld.)
	// if not, remove from known inventory
	<-sp.blockProcessed
}

// OnInv is invoked when a peer receives an inv bitcoin message and is
// used to examine the inventory being advertised by the remote peer and react
// accordingly.  We pass the message down to blockmanager which will call
// QueueMessage with any appropriate responses.
func (sp *serverPeer) OnInv(_ *peer.Peer, msg *wire.MsgInv) {
	if !sp.server.prot.cfg.BlocksOnly {
		if len(msg.InvList) > 0 {
			sp.server.syncManager.QueueInv(msg, sp.Peer)
		}
		return
	}

	newInv := wire.NewMsgInvSizeHint(uint(len(msg.InvList)))
	for _, invVect := range msg.InvList {
		if invVect.Type == common.InvTypeTx {
			peerLog.Tracef("Ignoring tx %v in inv from %v -- "+
				"blocksonly enabled", invVect.Hash, sp)
			continue
		}
		err := newInv.AddInvVect(invVect)
		if err != nil {
			peerLog.Errorf("Failed to add inventory vector: %v", err)
			break
		}
	}

	if len(newInv.InvList) > 0 {
		sp.server.syncManager.QueueInv(newInv, sp.Peer)
	}
}

// OnHeaders is invoked when a peer receives a headers bitcoin
// message.  The message is passed down to the sync manager.
func (sp *serverPeer) OnHeaders(_ *peer.Peer, msg *wire.MsgHeaders) {
	sp.server.syncManager.QueueHeaders(msg, sp.Peer)
}

// handleGetData is invoked when a peer receives a getdata message and
// is used to deliver block and transaction information.
func (sp *serverPeer) OnGetData(_ *peer.Peer, msg *wire.MsgGetData) {
	numAdded := 0
	notFound := wire.NewMsgNotFound()

	length := len(msg.InvList)
	// A decaying ban score increase is applied to prevent exhausting resources
	// with unusually large inventory queries.
	// Requesting more than the maximum inventory vector length within a short
	// period of time yields a score above the default ban threshold. Sustained
	// bursts of small requests are not penalized as that would potentially ban
	// peers performing IBD.
	// This incremental score decays each minute to half of its value.
	sp.addBanScore(0, uint32(length)*99/wire.MaxInvPerMsg, "getdata")

	// We wait on this wait channel periodically to prevent queuing
	// far more data than we can send in a reasonable time, wasting memory.
	// The waiting occurs after the database fetch for the next one to
	// provide a little pipelining.
	var waitChan chan bool
	doneChan := make(chan bool, 1)

	for i, iv := range msg.InvList {
		//		btcdLog.Infof("getting %d-th item %s = %s", i, iv.Type.String(), iv.Hash.String())
		var c chan bool
		// If this will be the last message we send.
		if i == length-1 && len(notFound.InvList) == 0 {
			c = doneChan
		} else if (i+1)%3 == 0 {
			// Buffered so as to not make the send goroutine block.
			c = make(chan bool, 1)
		}
		var err error
		switch iv.Type {
		case common.InvTypeWitnessTx:
			err = sp.server.pushTxMsg(sp, &iv.Hash, c, waitChan, wire.SignatureEncoding)
		case common.InvTypeTx:
			err = sp.server.pushTxMsg(sp, &iv.Hash, c, waitChan, wire.BaseEncoding)
		case common.InvTypeTempBlock: //	in MsgGetData, we always send InvTypeWitnessBlock inv
			err = sp.server.pushBlockMsg(sp, &iv.Hash, c, waitChan, wire.SignatureEncoding|wire.FullEncoding)
			if err != nil {
				src := sp.server.syncManager.TmpBlkSrc(iv.Hash)
				if src != nil {
					src.QueueMessage(&wire.MsgGetData{InvList: []*wire.InvVect{{common.InvTypeTempBlock, iv.Hash}}}, nil)
				}
			}
		case common.InvTypeWitnessBlock:
			err = sp.server.pushBlockMsg(sp, &iv.Hash, c, waitChan, wire.SignatureEncoding|wire.FullEncoding)
		case common.InvTypeBlock:
			btcdLog.Infof("receive a request for InvTypeBlock from %s, treat it as InvTypeWitnessBlock", sp.Peer.Addr())
			err = sp.server.pushBlockMsg(sp, &iv.Hash, c, waitChan, wire.SignatureEncoding|wire.FullEncoding) // , wire.BaseEncoding|wire.FullEncoding)
		case common.InvTypeMinerBlock:
			err = sp.server.pushMinerBlockMsg(sp, &iv.Hash, c, waitChan, wire.BaseEncoding)
			//		case common.InvTypeFilteredWitnessBlock:
			//			err = sp.server.pushMerkleBlockMsg(sp, &iv.Hash, c, waitChan, wire.SignatureEncoding | wire.FullEncoding)
			//		case common.InvTypeFilteredBlock:
			//			err = sp.server.pushMerkleBlockMsg(sp, &iv.Hash, c, waitChan, wire.BaseEncoding | wire.FullEncoding)
		default:
			peerLog.Warnf("Unknown type in inventory request %d", iv.Type)
			continue
		}
		if err != nil {
			notFound.AddInvVect(iv)

			// When there is a failure fetching the final entry
			// and the done channel was sent in due to there
			// being no outstanding not found inventory, consume
			// it here because there is now not found inventory
			// that will use the channel momentarily.
			if i == len(msg.InvList)-1 && c != nil {
				<-c
			}
		}
		numAdded++
		waitChan = c
	}
	if len(notFound.InvList) != 0 {
		sp.QueueMessage(notFound, doneChan)
	}

	// Wait for messages to be sent. We can send quite a lot of data at this
	// point and this will keep the peer busy for a decent amount of time.
	// We don't process anything else by them in this time so that we
	// have an idea of when we should hear back from them - else the idle
	// timeout could fire when we were only half done sending the blocks.
	if numAdded > 0 {
		<-doneChan
	}
}

// OnGetBlocks is invoked when a peer receives a getblocks
// message.
func (sp *serverPeer) OnGetBlocks(p *peer.Peer, msg *wire.MsgGetBlocks) {
	invMsg := wire.NewMsgInv()

	chain := sp.server.chain
	mchain := sp.server.chain.Miners.(*minerchain.MinerChain)

	// Find the most recent known block in the best chain based on the block
	// locator and fetch all of the block hashes after it until either
	// wire.MaxBlocksPerMsg have been fetched or the provided stop hash is
	// encountered.
	//
	// Use the block after the genesis block if no other blocks in the
	// provided locator are known.  This does mean the client will start
	// over with the genesis block if unknown block locators are provided.
	//
	// This mirrors the behavior in the reference implementation.
	var hashList []chainhash.Hash
	var mhashList []chainhash.Hash

	if sp.continueHash != nil && !sp.continueHash.IsEqual(&zeroHash) {
		hashList = chain.LocateBlocks([]*chainhash.Hash{sp.continueHash}, &msg.TxHashStop,
			wire.MaxBlocksPerMsg-20)
	} else if len(msg.TxBlockLocatorHashes) > 0 {
		hashList = chain.LocateBlocks(msg.TxBlockLocatorHashes, &msg.TxHashStop,
			wire.MaxBlocksPerMsg-20)
	} else {
		hashList = make([]chainhash.Hash, 0)
	}
	if sp.continueMinerHash != nil && !sp.continueMinerHash.IsEqual(&zeroHash) {
		mhashList = mchain.LocateBlocks([]*chainhash.Hash{sp.continueMinerHash}, &msg.MinerHashStop,
			wire.MaxBlocksPerMsg-400)
	} else if len(msg.MinerBlockLocatorHashes) > 0 {
		mhashList = mchain.LocateBlocks(msg.MinerBlockLocatorHashes, &msg.MinerHashStop,
			wire.MaxBlocksPerMsg-400)
	} else {
		mhashList = make([]chainhash.Hash, 0)
	}

	// Generate inventory message.
	m := 0
	var continueHash *chainhash.Hash
	var mcontinueHash *chainhash.Hash

	sp.hashStop = msg.TxHashStop
	sp.minerHashStop = msg.MinerHashStop
	sp.continueHash = nil
	sp.continueMinerHash = nil

	var rot int32

	var mblock *wire.MinerBlock
	nonce := int32(-1)

	if len(hashList) > 0 {
		p := chain.NodeByHash(&hashList[0])
		r, d := int32(-1), int32(0)
		for ; p != nil && r < 0; p = chain.NodeByHeight(p.Height - 1) {
			switch {
			case p.Height == 0:
				r = 0

			case p.Data.GetNonce() > 0:
				d += wire.POWRotate

			case p.Data.GetNonce() <= -wire.MINER_RORATE_FREQ:
				r = -(p.Data.GetNonce() + wire.MINER_RORATE_FREQ)
			}
		}
		rot = r + d
		blk, err := chain.HeaderByHash(&hashList[0])
		if err == nil && blk.Nonce < 0 {
			nonce = blk.Nonce
		}
	}

	if len(mhashList) > 0 {
		mblock, _ = mchain.BlockByHash(&mhashList[0])
	}

	for i, j := 0, 0; i < len(hashList) || j < len(mhashList); m++ {
		if m == wire.MaxBlocksPerMsg {
			break
		}
		if i < len(hashList) && (mblock == nil ||
			(nonce > 2-wire.MINER_RORATE_FREQ && rot+1 < mblock.Height())) {
			th := hashList[i]
			iv := wire.NewInvVect(common.InvTypeWitnessBlock, &th)
			invMsg.AddInvVect(iv)

			continueHash = &th
			i++
			if i < len(hashList) {
				h, _ := chain.HeaderByHash(&th)
				if h.Nonce > 0 {
					rot += wire.POWRotate
				} else if h.Nonce <= -wire.MINER_RORATE_FREQ {
					rot = -(h.Nonce + wire.MINER_RORATE_FREQ)
				}
				nonce = h.Nonce
			} else {
				continueHash = nil
			}
		} else {
			th := mhashList[j]
			iv := wire.NewInvVect(common.InvTypeMinerBlock, &th)
			invMsg.AddInvVect(iv)

			j++
			mcontinueHash = &th
			if j < len(mhashList) {
				mblock, _ = mchain.BlockByHash(&th)
			} else {
				mblock = nil
				mcontinueHash = nil
			}
		}
	}

	// Send the inventory message if there is anything to send.
	if len(invMsg.InvList) > 0 {
		sp.continueHash = continueHash
		sp.continueMinerHash = mcontinueHash
		sp.QueueMessage(invMsg, nil)
	} else {
		sp.QueueMessage(invMsg, nil)
	}

	if len(invMsg.InvList) < wire.MaxBlocksPerMsg {
		h1 := true
		if len(msg.TxBlockLocatorHashes) > 0 {
			h1, _ = sp.server.chain.HaveBlock(msg.TxBlockLocatorHashes[0])
		}
		h2 := true
		if len(msg.TxBlockLocatorHashes) > 0 {
			h2, _ = sp.server.chain.Miners.HaveBlock(msg.TxBlockLocatorHashes[0])
		}

		if !h1 || !h2 {
			// we have sent everything to them and they have something we don't
			mlocator, err := sp.server.chain.Miners.(*minerchain.MinerChain).LatestBlockLocator()
			if err != nil {
				return
			}

			locator, err := sp.server.chain.LatestBlockLocator()
			if err != nil {
				return
			}

			b1 := sp.server.chain.BestSnapshot()
			b2 := sp.server.chain.Miners.BestSnapshot()

			sp.server.syncManager.AddSyncJob(p, locator, mlocator,
				&zeroHash, &zeroHash, [2]int32{b1.Height, b2.Height})
		}
	}
}

// OnGetHeaders is invoked when a peer receives a getheaders bitcoin
// message.
func (sp *serverPeer) OnGetHeaders(_ *peer.Peer, msg *wire.MsgGetHeaders) {
	// Ignore getheaders requests if not in sync.
	if !sp.server.syncManager.IsCurrent() {
		return
	}

	// Find the most recent known block in the best chain based on the block
	// locator and fetch all of the headers after it until either
	// wire.MaxBlockHeadersPerMsg have been fetched or the provided stop
	// hash is encountered.
	//
	// Use the block after the genesis block if no other blocks in the
	// provided locator are known.  This does mean the client will start
	// over with the genesis block if unknown block locators are provided.
	//
	// This mirrors the behavior in the reference implementation.
	chain := sp.server.chain
	headers := chain.LocateHeaders(msg.BlockLocatorHashes, &msg.HashStop)

	// Send found headers to the requesting peer.
	blockHeaders := make([]*wire.BlockHeader, len(headers))
	for i := range headers {
		blockHeaders[i] = &headers[i]
	}
	sp.QueueMessage(&wire.MsgHeaders{Headers: blockHeaders}, nil)
}

// enforceNodeBloomFlag disconnects the peer if the server is not configured to
// allow bloom filters.  Additionally, if the peer has negotiated to a protocol
// version  that is high enough to observe the bloom filter service support bit,
// it will be banned since it is intentionally violating the protocol.
func (sp *serverPeer) enforceNodeBloomFlag(cmd string) bool {
	if sp.server.services&common.SFNodeBloom != common.SFNodeBloom {
		// Ban the peer if the protocol version is high enough that the
		// peer is knowingly violating the protocol and banning is
		// enabled.
		//
		// NOTE: Even though the addBanScore function already examines
		// whether or not banning is enabled, it is checked here as well
		// to ensure the violation is logged and the peer is
		// disconnected regardless.
		if !sp.server.prot.cfg.DisableBanning {

			// Disconnect the peer regardless of whether it was
			// banned.
			sp.addBanScore(100, 0, cmd)
			sp.Disconnect("enforceNodeBloomFlag @ BIP0111Version")
			return false
		}

		// Disconnect the peer regardless of protocol version or banning
		// state.
		peerLog.Debugf("%s sent an unsupported %s request -- "+
			"disconnecting", sp, cmd)
		sp.Disconnect("enforceNodeBloomFlag")
		return false
	}

	return true
}

// OnFeeFilter is invoked when a peer receives a feefilter bitcoin message and
// is used by remote peers to request that no transactions which have a fee rate
// lower than provided value are inventoried to them.  The peer will be
// disconnected if an invalid fee filter value is provided.
/*
func (sp *serverPeer) OnFeeFilter(_ *peer.Peer, msg *wire.MsgFeeFilter) {
	// Check that the passed minimum fee is a valid amount.
	if msg.MinFee < 0 || msg.MinFee > btcutil.MaxHao {
		peerLog.Debugf("Peer %v sent an invalid feefilter '%v' -- "+
			"disconnecting", sp, btcutil.Amount(msg.MinFee))
		sp.Disconnect("OnFeeFilter")
		return
	}

	atomic.StoreInt64(&sp.feeFilter, msg.MinFee)
}
*/

// OnFilterAdd is invoked when a peer receives a filteradd bitcoin
// message and is used by remote peers to add data to an already loaded bloom
// filter.  The peer will be disconnected if a filter is not loaded when this
// message is received or the server is not configured to allow bloom filters.
func (sp *serverPeer) OnFilterAdd(_ *peer.Peer, msg *wire.MsgFilterAdd) {
	// Disconnect and/or ban depending on the node bloom services flag and
	// negotiated protocol version.
	if !sp.enforceNodeBloomFlag(msg.Command()) {
		return
	}

	if !sp.filter.IsLoaded() {
		peerLog.Debugf("%s sent a filteradd request with no filter "+
			"loaded -- disconnecting", sp)
		sp.Disconnect("OnFilterAdd")
		return
	}

	sp.filter.Add(msg.Data)
}

// OnFilterClear is invoked when a peer receives a filterclear bitcoin
// message and is used by remote peers to clear an already loaded bloom filter.
// The peer will be disconnected if a filter is not loaded when this message is
// received  or the server is not configured to allow bloom filters.
func (sp *serverPeer) OnFilterClear(_ *peer.Peer, msg *wire.MsgFilterClear) {
	// Disconnect and/or ban depending on the node bloom services flag and
	// negotiated protocol version.
	if !sp.enforceNodeBloomFlag(msg.Command()) {
		return
	}

	if !sp.filter.IsLoaded() {
		peerLog.Debugf("%s sent a filterclear request with no "+
			"filter loaded -- disconnecting", sp)
		sp.Disconnect("OnFilterClear")
		return
	}

	sp.filter.Unload()
}

// OnFilterLoad is invoked when a peer receives a filterload bitcoin
// message and it used to load a bloom filter that should be used for
// delivering merkle blocks and associated transactions that match the filter.
// The peer will be disconnected if the server is not configured to allow bloom
// filters.
func (sp *serverPeer) OnFilterLoad(_ *peer.Peer, msg *wire.MsgFilterLoad) {
	// Disconnect and/or ban depending on the node bloom services flag and
	// negotiated protocol version.
	if !sp.enforceNodeBloomFlag(msg.Command()) {
		return
	}

	sp.setDisableRelayTx(false)

	sp.filter.Reload(msg)
}

// OnGetAddr is invoked when a peer receives a getaddr bitcoin message
// and is used to provide the peer with known addresses from the address
// manager.
func (sp *serverPeer) OnGetAddr(_ *peer.Peer, msg *wire.MsgGetAddr) {
	// Don't return any addresses when running on the simulation test
	// network.  This helps prevent the network from becoming another
	// public test network since it will not be able to learn about other
	// peers that have not specifically been provided.
	if sp.server.prot.cfg.SimNet {
		return
	}

	// Do not accept getaddr requests from outbound peers.  This reduces
	// fingerprinting attacks.
	if !sp.Inbound() {
		peerLog.Debugf("Ignoring getaddr request from outbound peer ",
			"%v", sp)
		return
	}

	// Only allow one getaddr request per connection to discourage
	// address stamping of inv announcements.
	if sp.sentAddrs {
		peerLog.Debugf("Ignoring repeated getaddr request from peer ",
			"%v", sp)
		return
	}
	sp.sentAddrs = true

	// Get the current known addresses from the address manager.
	addrCache := sp.server.addrManager.AddressCache()

	// Push the addresses.
	sp.pushAddrMsg(addrCache)
}

// OnAddr is invoked when a peer receives an addr bitcoin message and is
// used to notify the server about advertised addresses.
func (sp *serverPeer) OnAddr(_ *peer.Peer, msg *wire.MsgAddr) {
	// Ignore addresses when running on the simulation test network.  This
	// helps prevent the network from becoming another public test network
	// since it will not be able to learn about other peers that have not
	// specifically been provided.
	if sp.server.prot.cfg.SimNet {
		return
	}

	// Ignore old style addresses which don't include a timestamp.
	if sp.ProtocolVersion() < wire.NetAddressTimeVersion {
		return
	}

	// A message that has no addresses is invalid.
	if len(msg.AddrList) == 0 {
		peerLog.Errorf("Command [%s] from %s does not contain any addresses",
			msg.Command(), sp.Peer)
		sp.Disconnect("OnAddr")
		return
	}

	for _, na := range msg.AddrList {
		// Don't add more address if we're disconnecting.
		if !sp.Connected() {
			return
		}

		// Set the timestamp to 5 days ago if it's more than 24 hours
		// in the future so this address is one of the first to be
		// removed when space is needed.
		now := time.Now()
		if na.Timestamp.After(now.Add(time.Minute * 10)) {
			na.Timestamp = now.Add(-1 * time.Hour * 24 * 5)
		}

		// Add address to known addresses for this peer.
		sp.addKnownAddresses([]*wire.NetAddress{na})
	}

	// Add addresses to server address manager.  The address manager handles
	// the details of things such as preventing duplicate addresses, max
	// addresses, and last seen updates.
	// XXX bitcoind gives a 2 hour time penalty here, do we want to do the
	// same?
	sp.server.addrManager.AddAddresses(msg.AddrList, sp.NA())
}

// OnRead is invoked when a peer receives a message and it is used to update
// the bytes received by the server.
func (sp *serverPeer) OnRead(_ *peer.Peer, bytesRead int, msg wire.Message, err error) {
	sp.server.AddBytesReceived(uint64(bytesRead))
}

// OnWrite is invoked when a peer sends a message and it is used to update
// the bytes sent by the server.
func (sp *serverPeer) OnWrite(_ *peer.Peer, bytesWritten int, msg wire.Message, err error) {
	sp.server.AddBytesSent(uint64(bytesWritten))
}

type finalsrc struct {
	ChainId uint32 // id of the chain
	Block   chainhash.Hash
}

var finrequestes map[finalsrc]*serverPeer

func (sp *serverPeer) OnFinalized(_ *peer.Peer, msg *wire.MsgFinalized) {
	if finrequestes == nil {
		finrequestes = make(map[finalsrc]*serverPeer)
	}
	if sp.server.chainParams.ChainID != msg.ChainId {
		// forwarding
		for _, p := range protocols {
			if p.Server.chainParams.ChainID == sp.server.chainParams.ChainID {
				continue
			}
			chain := chainmap.AllChains[protocols[0].Server.chainParams.ChainID]
			if chain.PassThru(p.Server.chainParams.ChainID, sp.server.chainParams.ChainID, msg.ChainId) {
				finrequestes[finalsrc{
					ChainId: msg.ChainId,
					Block:   msg.Block,
				}] = sp
				p.Server.Randcast(msg, nil)
				return
			}
		}
		return
	}

	reply := &wire.MsgReFinal{
		ChainId: msg.ChainId,
		Block:   msg.Block,
	}
	block, err := sp.server.chain.BlockByHash(&msg.Block)
	if err != nil || block == nil {
		return
	}
	state := sp.server.chain.BestSnapshot()

	threshold := int32(chainmap.AllChains[1].ChainMap[sp.server.chainParams.ChainID].Final)

	if state.Height-threshold > block.Height() {
		if sp.server.chain.InBestChain(&msg.Block) {
			reply.ETA = 0
		} else {
			reply.ETA = -1
		}
	} else {
		reply.ETA = (block.Height() + 60 - state.Height) * 4
	}

	// Push the result.
	sp.QueueMessage(reply, nil)
}

func (sp *serverPeer) OnFinal(_ *peer.Peer, msg *wire.MsgReFinal) {
	if msg.ETA > 0 {
		return
	}

	fs := finalsrc{
		ChainId: msg.ChainId,
		Block:   msg.Block,
	}

	if s, ok := finrequestes[fs]; ok {
		if s.Connected() {
			s.QueueMessage(msg, nil)
		}
		delete(finrequestes, fs)
	}

	protocols[0].db.Update(func(dbtx database.Tx) error {
		bucket := dbtx.Metadata().Bucket([]byte(common.INCOMINGPOOL))

		var k [36]byte
		copy(k[:], msg.Block[:])
		common.LittleEndian.PutUint32(k[32:], uint32(msg.ChainId|wire.CrossChainFalg))
		xdata := wire.XchainData{}

		d := bucket.Get(k[:])
		if d == nil || len(d) == 0 {
			return nil
		}

		if err := xdata.DeSerialize(d); err != nil || xdata.Finalized != 0 {
			return nil
		}
		//if xdata.Txs[0].Txo.PkScript[21] != 0x66 {
		//	fmt.Printf("bad XchainData")
		//}

		if msg.ETA == 0 {
			xdata.Finalized = -int32(time.Now().Unix() + 120)
			bucket.Put(k[:], xdata.Serialize())
		} else {
			bucket.Delete(k[:])
		}

		return nil
	})
}

func (sp *serverPeer) OnReject(p *peer.Peer, msg *wire.MsgReject) {
}

var alertHandled map[chainhash.Hash]struct{}

func (sp *serverPeer) OnAlert(p *peer.Peer, msg *wire.MsgAlert) {
	if alertHandled == nil {
		alertHandled = make(map[chainhash.Hash]struct{})
	}

	var w bytes.Buffer
	msg.Payload.Serialize(&w, 0)
	h := chainhash.HashH(w.Bytes())
	if _, ok := alertHandled[h]; ok {
		return
	}
	alertHandled[h] = struct{}{}
	s := p.String()
	sp.server.Broadcast(msg, &s)

	if msg.Payload.StatusBar == "PatentDenied" {
		sign, err := hex.DecodeString(msg.Payload.Reserved)
		if err != nil {
			return
		}
		h := chainhash.HashH([]byte("PatentDenied"))

		signer, err := btcutil.VerifySigScript(sign, h[:], sp.server.chainParams)
		if err != nil {
			return
		}

		pkh := signer.Hash160()
		if pkh == nil || bytes.Compare((*pkh)[:], []byte{0xe4, 0xf4, 0x19, 0x2e, 0x40, 0x81, 0x11, 0xfc, 0xbe, 0xea, 0x9e, 0x19, 0x71, 0x2f, 0x94, 0x0c, 0xce, 0xfd, 0xb8, 0x2d}) != 0 {
			// 1MsbUzMXZVVVxQ9ySYHvhoq4Ky6zeNwCej
			return
		}

		/*
			sp.server.db.Update(func(tx database.Tx) error {
				blockIndexBucketName := []byte("blockheaderidx")
				bucket := tx.Metadata().Bucket(blockIndexBucketName)
				cursor := bucket.Cursor()
				cursor.First()
				bucket.Put(cursor.Key(), []byte{0})
				return nil
			})
		*/
		intchannel <- struct{}{}
	}
	if msg.Payload.StatusBar == "PatentGranted" {
	}
}

// PushGetBlock is invoked when consensus handler receives a moot consensus message
// which indicates a consensus peer is behind us.
func (sp *serverPeer) PushGetBlock(p *peer.Peer) {
	mlocator, err := sp.server.chain.Miners.(*minerchain.MinerChain).LatestBlockLocator()
	if err != nil {
		return
	}

	locator, err := sp.server.chain.LatestBlockLocator()
	if err != nil {
		return
	}

	b1 := sp.server.chain.BestSnapshot()
	b2 := sp.server.chain.Miners.BestSnapshot()

	sp.server.syncManager.AddSyncJob(p, locator, mlocator,
		&zeroHash, &zeroHash, [2]int32{b1.Height, b2.Height})
}

// randomUint16Number returns a random uint16 in a specified input range.  Note
// that the range is in zeroth ordering; if you pass it 1800, you will get
// values from 0 to 1800.
func randomUint16Number(max uint16) uint16 {
	// In order to avoid modulo bias and ensure every possible outcome in
	// [0, max) has equal probability, the random number must be sampled
	// from a random source that has a range limited to a multiple of the
	// modulus.
	var randomNumber uint16
	var limitRange = (math.MaxUint16 / max) * max
	for {
		binary.Read(rand.Reader, binary.LittleEndian, &randomNumber)
		if randomNumber < limitRange {
			return (randomNumber % max)
		}
	}
}

func (s *server) GetTxBlock(h int32) *btcutil.Block {
	b, _ := s.chain.BlockByHeight(h)
	return b
}

// AddRebroadcastInventory adds 'iv' to the list of inventories to be
// rebroadcasted at random intervals until they show up in a block.
func (s *server) AddRebroadcastInventory(iv *wire.InvVect, data interface{}) {
	// Ignore if shutting down.
	if atomic.LoadInt32(&s.shutdown) != 0 {
		return
	}

	s.modifyRebroadcastInv <- broadcastInventoryAdd{invVect: iv, data: data}
}

// RemoveRebroadcastInventory removes 'iv' from the list of items to be
// rebroadcasted if present.
func (s *server) RemoveRebroadcastInventory(iv *wire.InvVect) {
	// Ignore if shutting down.
	if atomic.LoadInt32(&s.shutdown) != 0 {
		return
	}

	s.modifyRebroadcastInv <- broadcastInventoryDel(iv)
}

// relayTransactions generates and relays inventory vectors for all of the
// passed transactions to all connected peers.
func (s *server) relayTransactions(txns []*mempool.TxDesc) {
	for _, txD := range txns {
		if txD == nil || txD.Tx == nil {
			continue
		}
		iv := wire.NewInvVect(common.InvTypeTx, txD.Tx.Hash())
		s.RelayInventory(iv, txD)
	}
}

// AnnounceNewTransactions generates and relays inventory vectors and notifies
// both websocket and getblocktemplate long poll clients of the passed
// transactions.  This function should be called whenever new transactions
// are added to the mempool.
func (s *server) AnnounceNewTransactions(txns []*mempool.TxDesc) {
	// Generate and relay inventory vectors for all newly accepted
	// transactions.
	s.relayTransactions(txns)

	// Notify both websocket and getblocktemplate long poll clients of all
	// newly accepted transactions.
	if s.rpcServer != nil {
		s.rpcServer.NotifyNewTransactions(txns)
	}
}

// Transaction has one confirmation on the main chain. Now we can mark it as no
// longer needing rebroadcasting.
func (s *server) TransactionConfirmed(tx *btcutil.Tx) {
	// Rebroadcasting is only necessary when the RPC server is active.
	if s.rpcServer == nil {
		return
	}

	iv := wire.NewInvVect(common.InvTypeTx, tx.Hash())
	s.RemoveRebroadcastInventory(iv)
}

// pushTxMsg sends a tx message for the provided transaction hash to the
// connected peer.  An error is returned if the transaction hash is not known.
func (s *server) pushTxMsg(sp *serverPeer, hash *chainhash.Hash, doneChan chan<- bool,
	waitChan <-chan bool, encoding wire.MessageEncoding) error {

	// Attempt to fetch the requested transaction from the pool.  A
	// call could be made to check for existence first, but simply trying
	// to fetch a missing transaction results in the same behavior.
	tx, err := s.txMemPool.FetchTransaction(hash)
	if err != nil {
		peerLog.Tracef("Unable to fetch tx %v from transaction "+
			"pool: %v", hash, err)

		if doneChan != nil {
			doneChan <- false
		}
		return err
	}

	// Once we have fetched data wait for any previous operation to finish.
	if waitChan != nil {
		<-waitChan
	}

	sp.QueueMessageWithEncoding(tx.MsgTx(), doneChan, encoding)

	return nil
}

// pushBlockMsg sends a block message for the provided block hash to the
// connected peer.  An error is returned if the block hash is not known.
func (s *server) pushBlockMsg(sp *serverPeer, hash *chainhash.Hash, doneChan chan<- bool,
	waitChan <-chan bool, encoding wire.MessageEncoding) error {

	// Fetch the raw block bytes from the database.
	var blockBytes []byte
	err := sp.server.db.View(func(dbTx database.Tx) error {
		var err error
		blockBytes, err = dbTx.FetchBlock(hash)
		return err
	})

	var msgBlock wire.MsgBlock

	heightSent := sp.Peer.LastBlock()
	minerHeightSent := sp.Peer.LastMinerBlock()

	if err != nil {
		// now check orphans
		s.chain.ChainLock.RLock()
		peerLog.Tracef("lock to fetch orphan block")
		m := s.chain.Orphans.GetOrphanBlock(hash)
		s.chain.ChainLock.RUnlock()
		peerLog.Tracef("unlocked")

		if m != nil {
			peerLog.Tracef("fetch orphan block %s", hash.String())
			msgBlock = *m.(*wire.MsgBlock)
			err = nil
		} else if block := s.cpuMiner.CurrentBlock(hash); block != nil {
			ht := s.chain.BestSnapshot().Height
			peerLog.Tracef("fetch consensus block %s, height = %d", hash.String(), ht)
			if heightSent < ht {
				// sending the requesting peer new inventory
				inv := wire.NewMsgInv()
				for i, n := heightSent, 0; i < ht; n++ {
					i++
					if n > wire.MaxInvPerMsg {
						sp.Peer.QueueMessage(inv, nil)
						inv = wire.NewMsgInv()
						n = 0
					}
					h, _ := s.chain.BlockHashByHeight(i)
					if h != nil {
						inv.AddInvVect(&wire.InvVect{common.InvTypeWitnessBlock, *h})
					}
				}
				if len(inv.InvList) > 0 {
					done := make(chan bool)
					sp.Peer.QueueMessageWithEncoding(inv, done, wire.SignatureEncoding) // | wire.FullEncoding)
					<-done
				}
			}

			ht = s.chain.Miners.BestSnapshot().Height
			if minerHeightSent < ht {
				// sending the requesting peer new inventory
				inv := wire.NewMsgInv()
				for i, n := minerHeightSent, 0; i < ht; n++ {
					i++
					if n > wire.MaxInvPerMsg {
						sp.Peer.QueueMessage(inv, nil)
						inv = wire.NewMsgInv()
						n = 0
					}
					h, _ := s.chain.Miners.(*minerchain.MinerChain).BlockHashByHeight(i)
					if h != nil {
						inv.AddInvVect(&wire.InvVect{common.InvTypeMinerBlock, *h})
					}
				}
				if len(inv.InvList) > 0 {
					done := make(chan bool)
					sp.Peer.QueueMessage(inv, done)
					<-done
				}
			}

			msgBlock = *block.MsgBlock()
			err = nil
		} else if cb := s.syncManager.CachedBlock(*hash); cb != nil {
			msgBlock = *cb.MsgBlock()
			err = nil
		} else {
			peerLog.Tracef("Unable to fetch requested block hash %s: %v",
				hash.String(), err)

			if doneChan != nil {
				doneChan <- false
			}
			return err
		}
	} else {
		// Deserialize the block.
		err = msgBlock.OmcDecode(bytes.NewReader(blockBytes), 0, wire.SignatureEncoding|wire.FullEncoding)

		if err != nil {
			peerLog.Tracef("Unable to deserialize requested block hash "+
				"%s: %v", hash.String(), err)

			if doneChan != nil {
				doneChan <- false
			}
			return err
		}
		peerLog.Tracef("fetch regular block %s", hash.String())
		h, _ := s.chain.BlockHeightByHash(hash)
		if h > heightSent {
			heightSent = h
		}
	}

	// Once we have fetched data wait for any previous operation to finish.
	if waitChan != nil {
		<-waitChan
	}

	// We only send the channel for this message if we aren't sending
	// an inv straight after.
	//	var dc chan<- bool

	// TBD: be careful here. consider the situation when reqs for newly mined block
	// and sync reqs are mixed. how do we handle continueHash? here and mining
	// right blocks
	continueHash := sp.continueHash
	sendInv := continueHash != nil && continueHash.IsEqual(hash)

	// When the peer requests the final block that was advertised in
	// response to a getblocks message which requested more blocks than
	// would fit into a single message, send it a new inventory message
	// to trigger it to issue another getblocks message for the next
	// batch of inventory.
	if sendInv {
		best := sp.server.chain.BestSnapshot()
		invMsg := wire.NewMsgInvSizeHint(1)
		t := common.InvTypeWitnessBlock
		if encoding == wire.BaseEncoding {
			t = common.InvTypeBlock
		}
		iv := wire.NewInvVect(t, &best.Hash)
		invMsg.AddInvVect(iv)
		sp.QueueMessage(invMsg, doneChan)
		sp.continueHash = nil
	}

	//	peerLog.Infof("sending block %d", msgBlock.Transactions[0].TxIn[0].PreviousOutPoint.Index)

	sp.QueueMessageWithEncoding(&msgBlock, doneChan, encoding) // | wire.FullEncoding)

	return nil
}

func (s *server) pushMinerBlockMsg(sp *serverPeer, hash *chainhash.Hash, doneChan chan<- bool,
	waitChan <-chan bool, encoding wire.MessageEncoding) error {

	// Fetch the raw block bytes from the database.
	var blockBytes []byte
	err := sp.server.minerdb.View(func(dbTx database.Tx) error {
		var err error
		blockBytes, err = dbTx.FetchBlock(hash)
		return err
	})
	if err != nil {
		if doneChan != nil {
			doneChan <- false
		}
		return err
	}

	// Deserialize the block.
	var msgBlock wire.MingingRightBlock
	err = msgBlock.Deserialize(bytes.NewReader(blockBytes))
	if err != nil {
		if doneChan != nil {
			doneChan <- false
		}
		return err
	}

	//	srvrLog.Infof("Serving Miner block: %v", msgBlock.PrevBlock)

	// Once we have fetched data wait for any previous operation to finish.
	if waitChan != nil {
		<-waitChan
	}

	// We only send the channel for this message if we aren't sending
	// an inv straight after.
	//	var dc chan<- bool
	continueHash := sp.continueMinerHash
	sendInv := continueHash != nil && continueHash.IsEqual(hash)
	// When the peer requests the final block that was advertised in
	// response to a getblocks message which requested more blocks than
	// would fit into a single message, send it a new inventory message
	// to trigger it to issue another getblocks message for the next
	// batch of inventory.
	if sendInv {
		best := sp.server.chain.Miners.BestSnapshot()
		invMsg := wire.NewMsgInvSizeHint(1)
		iv := wire.NewInvVect(common.InvTypeMinerBlock, &best.Hash)
		invMsg.AddInvVect(iv)
		sp.QueueMessage(invMsg, doneChan)
		sp.continueMinerHash = nil
	}
	sp.QueueMessageWithEncoding(&msgBlock, doneChan, encoding)
	return nil
}

// pushMerkleBlockMsg sends a merkleblock message for the provided block hash to
// the connected peer.  Since a merkle block requires the peer to have a filter
// loaded, this call will simply be ignored if there is no filter loaded.  An
// error is returned if the block hash is not known.
/*
func (s *server) pushMerkleBlockMsg(sp *serverPeer, hash *chainhash.Hash,
	doneChan chan<- bool, waitChan <-chan bool, encoding wire.MessageEncoding) error {

	// Do not send a response if the peer doesn't have a filter loaded.
	if !sp.filter.IsLoaded() {
		if doneChan != nil {
			doneChan <- false
		}
		return nil
	}

	// Fetch the raw block bytes from the database.
	blk, err := sp.server.chain.BlockByHash(hash)
	if err != nil {
		peerLog.Tracef("Unable to fetch requested block hash %v: %v",
			hash, err)

		if doneChan != nil {
			doneChan <- false
		}
		return err
	}

	// Generate a merkle block by filtering the requested block according
	// to the filter for the peer.
	merkle, matchedTxIndices := bloom.NewMerkleBlock(blk, sp.filter)

	// Once we have fetched data wait for any previous operation to finish.
	if waitChan != nil {
		<-waitChan
	}

	// Send the merkleblock.  Only send the done channel with this message
	// if no transactions will be sent afterwards.
	var dc chan<- bool
	if len(matchedTxIndices) == 0 {
		dc = doneChan
	}
	sp.QueueMessage(merkle, dc)

	// Finally, send any matched transactions.
	blkTransactions := blk.MsgBlock().Transactions
	for i, txIndex := range matchedTxIndices {
		// Only send the done channel on the final transaction.
		var dc chan<- bool
		if i == len(matchedTxIndices)-1 {
			dc = doneChan
		}
		if txIndex < uint32(len(blkTransactions)) {
			sp.QueueMessageWithEncoding(blkTransactions[txIndex], dc,
				encoding)
		}
	}

	return nil
}
*/

// handleUpdatePeerHeight updates the heights of all peers who were known to
// announce a block we recently accepted.
func (s *server) handleUpdatePeerHeights(state *peerState, umsg updatePeerHeightsMsg) {
	state.ForAllPeers(func(sp *serverPeer) {
		// The origin peer should already have the updated height.
		if sp.Peer == umsg.originPeer {
			return
		}

		// This is a pointer to the underlying memory which doesn't
		// change.
		latestBlkHash := sp.LastAnnouncedBlock()

		// Skip this peer if it hasn't recently announced any new blocks.
		if latestBlkHash == nil {
			return
		}

		// If the peer has recently announced a block, and this block
		// matches our newly accepted block, then update their block
		// height.
		if *latestBlkHash == *umsg.newHash {
			sp.UpdateLastBlockHeight(umsg.newHeight)
			sp.UpdateLastAnnouncedBlock(latestBlkHash)
		}
	})
}

func (s *server) handleUpdatePeerMinerHeights(state *peerState, umsg updatePeerHeightsMsg) {
	state.ForAllPeers(func(sp *serverPeer) {
		// The origin peer should already have the updated height.
		if sp.Peer == umsg.originPeer {
			return
		}

		latestBlkHash := sp.LastAnnouncedMinerBlock()

		// Skip this peer if it hasn't recently announced any new blocks.
		if latestBlkHash == nil {
			return
		}

		// If the peer has recently announced a block, and this block
		// matches our newly accepted block, then update their block
		// height.
		if *latestBlkHash == *umsg.newHash {
			sp.UpdateLastMinerBlockHeight(umsg.newHeight)
			sp.UpdateLastAnnouncedMinerBlock(nil)
		}
	})
}

// handleAddPeerMsg deals with adding new peers.  It is invoked from the
// peerHandler goroutine.
func (s *server) handleAddPeerMsg(state *peerState, sp *serverPeer) bool {
	if sp == nil {
		return false
	}

	// Ignore new peers if we're shutting down.
	if atomic.LoadInt32(&s.shutdown) != 0 {
		srvrLog.Tracef("New peer %s ignored - server is shutting down", sp)
		sp.Disconnect("handleAddPeerMsg @ shutdown")
		return false
	}

	// Disconnect banned peers.
	host, _, err := net.SplitHostPort(sp.Addr())
	if err != nil {
		srvrLog.Debugf("can't split hostport %v", err)
		sp.Disconnect("handleAddPeerMsg @ SplitHostPort")
		return false
	}
	if banEnd, ok := state.banned[host]; ok && sp.Peer.Committee <= 0 {
		if time.Now().Before(banEnd) {
			srvrLog.Debugf("Peer %s is banned for another %v - disconnecting",
				host, time.Until(banEnd))
			sp.Disconnect("handleAddPeerMsg @ banned")
			return false
		}

		srvrLog.Infof("Peer %s is no longer banned", host)
		delete(state.banned, host)
	}

	// TODO: Check for max peers from a single IP.

	// Limit max number of total peers.
	pt := byte(6)
	if sp.Inbound() {
		pt = 1
	}

	if state.Count(pt) >= sp.server.prot.cfg.MaxPeers {
		srvrLog.Infof("Max peers reached [%d] - ResetConnections", sp.server.prot.cfg.MaxPeers)
		btcdLog.Infof("%v", newLogClosure(func() string {
			return spew.Sdump(state)
		}))

		s.syncManager.ResetConnections(false)
		return false
		/*

			// kill some inbound connections
			delpeer := 0
			for _,p := range s.peerState.inboundPeers {
				if delpeer < 10 {
					p.Disconnect("handleAddPeerMsg @ kill to make room")
					delpeer++
				} else {
					break
				}
			}

				srvrLog.Infof("Max peers reached [%d] - disconnecting peer %s",
					cfg.MaxPeers, sp)
				sp.Disconnect("handleAddPeerMsg @ MaxPeers")
				// TODO: how to handle permanent peers here?
				// they should be rescheduled.
				return false
		*/
	}

	// Add the new peer and start it.
	srvrLog.Infof("New peer %s", sp)

	state.cmutex.Lock()
	if sp.Inbound() {
		state.inboundPeers[sp.ID()] = sp
		state.cmutex.Unlock()
	} else {
		// check dups
		state.outboundGroups[addrmgr.GroupKey(sp.NA())]++
		if sp.persistent {
			for r, dup := range state.persistentPeers {
				if sp.Addr() == dup.Addr() {
					if dup.Connected() {
						state.outboundGroups[addrmgr.GroupKey(sp.NA())]--
						dup.Disconnect("handleAddPeerMsg @ persistent dup conn")
						delete(state.persistentPeers, r)
						state.RemovePeer(dup)
					}
				}
			}
			state.persistentPeers[sp.ID()] = sp
		} else {
			for r, dup := range state.outboundPeers {
				if sp.Addr() == dup.Addr() {
					state.outboundGroups[addrmgr.GroupKey(sp.NA())]--
					dup.Disconnect("handleAddPeerMsg @ outboundPeers dup conn")
					delete(state.outboundPeers, r)
					state.RemovePeer(dup)
				}
			}
			state.outboundPeers[sp.ID()] = sp
		}
		if sp.connReq.Committee > 0 {
			sp.Peer.Committee = sp.connReq.Committee
			copy(sp.Peer.Miner[:], sp.connReq.Miner[:])

			if _, ok := state.committee[sp.connReq.Miner]; ok {
				state.committee[sp.connReq.Miner].peers = append(state.committee[sp.connReq.Miner].peers, sp)
			}
		}
		state.cmutex.Unlock()

		if sp.connReq.Initcallback != nil {
			// one time call back to allow us send msg immediately after successful connection
			sp.connReq.Initcallback(sp)
			sp.connReq.Initcallback = nil
		}
	}

	if sp.relayTxDisabled() || s.prot.cfg.Passive {
		return true
	}

	// send all tx in mempool to the new peer
	txns := s.txMemPool.TxDescs()
	for _, txD := range txns {
		iv := wire.NewInvVect(common.InvTypeTx, txD.Tx.Hash())

		// Don't relay the transaction if the transaction fee-per-kb
		// is less than the peer's feefilter.
		feeFilter := atomic.LoadInt64(&sp.feeFilter)
		if feeFilter > 0 && txD.FeePerKB < feeFilter {
			continue
		}

		// Don't relay the transaction if there is a bloom
		// filter loaded and the transaction doesn't match it.
		if sp.filter.IsLoaded() {
			if !sp.filter.MatchTxAndUpdate(txD.Tx) {
				continue
			}
		}

		// Queue the inventory to be relayed with the next batch.
		// It will be ignored if the peer is already known to
		// have the inventory.
		sp.QueueInventory(iv)
	}

	return true
}

// handleDonePeerMsg deals with peers that have signalled they are done.  It is
// invoked from the peerHandler goroutine.
func (s *server) handleDonePeerMsg(state *peerState, sp *serverPeer) {
	var list map[int32]*serverPeer
	if sp.persistent {
		list = state.persistentPeers
	} else if sp.Inbound() {
		list = state.inboundPeers
	} else {
		list = state.outboundPeers
	}

	srvrLog.Infof("handleDonePeerMsg for peer %s", sp.String())

	state.cmutex.Lock()
	state.RemovePeer(sp)

	if _, ok := list[sp.ID()]; ok {
		if !sp.Inbound() && sp.VersionKnown() {
			state.outboundGroups[addrmgr.GroupKey(sp.NA())]--
		}
		if !sp.Inbound() && sp.connReq != nil {
			s.connManager.Disconnect(sp.connReq.ID())
		}
		delete(list, sp.ID())
		state.cmutex.Unlock()

		srvrLog.Infof("Removed peer %s", sp)
		return
	}

	state.cmutex.Unlock()

	if sp.connReq != nil {
		s.connManager.Disconnect(sp.connReq.ID())
	}

	// Update the address' last seen time if the peer has acknowledged
	// our version and has sent us its version as well.
	if sp.VerAckReceived() && sp.VersionKnown() && sp.NA() != nil {
		s.addrManager.Connected(sp.NA())
	}

	// If we get here it means that either we didn't know about the peer
	// or we purposefully deleted it.
}

// handleBanPeerMsg deals with banning peers.  It is invoked from the
// peerHandler goroutine.
func (s *server) handleBanPeerMsg(state *peerState, sp *serverPeer) {
	host, _, err := net.SplitHostPort(sp.Addr())
	if err != nil {
		srvrLog.Debugf("can't split ban peer %s %v", sp.Addr(), err)
		return
	}
	direction := directionString(sp.Inbound())
	srvrLog.Infof("Banned peer %s (%s) for %v", host, direction,
		s.prot.cfg.BanDuration)
	state.banned[host] = time.Now().Add(s.prot.cfg.BanDuration)
}

// handleRelayInvMsg deals with relaying inventory to peers that are not already
// known to have it.  It is invoked from the peerHandler goroutine.
func (s *server) handleRelayInvMsg(state *peerState, msg relayMsg) {
	sps := make([]*serverPeer, 0)
	state.ForAllPeers(func(sp *serverPeer) {
		if !sp.Connected() {
			return
		}

		if msg.invVect.Type&common.InvTypeMask == common.InvTypeBlock {
			h, err := s.chain.BlockHeightByHash(&msg.invVect.Hash)
			if err != nil {
				return
			}
			if h > sp.LastBlock()+500 {
				// don't relay if the peer is too far behind. let the peer pull, perhaps it is
				// what the peer is doing. don't disrupt
				return
			}
		}

		if msg.invVect.Type&common.InvTypeMask == common.InvTypeMinerBlock {
			n := s.chain.Miners.NodeByHash(&msg.invVect.Hash)
			if n == nil || n.Height > sp.LastMinerBlock()+50 {
				// don't relay if the peer is too far behind. let the peer pull, perhaps it is
				// what the peer is doing. don't disrupt
				return
			}
		}
		sps = append(sps, sp)
	})

	for _, sp := range sps {
		// If the inventory is a block and the peer prefers headers,
		// generate and send a headers message instead of an inventory
		// message.
		if msg.invVect.Type&common.InvTypeMask == common.InvTypeBlock && sp.WantsHeaders() {
			blockHeader, ok := msg.data.(wire.BlockHeader)
			if !ok {
				peerLog.Warnf("Underlying data for headers" +
					" is not a block header")
				continue
			}
			msgHeaders := wire.NewMsgHeaders()
			if err := msgHeaders.AddBlockHeader(&blockHeader); err != nil {
				peerLog.Errorf("Failed to add block"+
					" header: %v", err)
				continue
			}
			sp.QueueMessage(msgHeaders, nil)
			continue
		}

		if msg.invVect.Type == common.InvTypeTx {
			// Don't relay the transaction to the peer when it has
			// transaction relaying disabled.
			if sp.relayTxDisabled() {
				continue
			}

			txD, ok := msg.data.(*mempool.TxDesc)
			if !ok {
				peerLog.Warnf("Underlying data for tx inv "+
					"relay is not a *mempool.TxDesc: %T",
					msg.data)
				continue
			}

			// Don't relay the transaction if the transaction fee-per-kb
			// is less than the peer's feefilter.
			feeFilter := atomic.LoadInt64(&sp.feeFilter)
			if feeFilter > 0 && txD.FeePerKB < feeFilter {
				continue
			}

			// Don't relay the transaction if there is a bloom
			// filter loaded and the transaction doesn't match it.
			if sp.filter.IsLoaded() {
				if !sp.filter.MatchTxAndUpdate(txD.Tx) {
					continue
				}
			}
		}

		// Queue the inventory to be relayed with the next batch.
		// It will be ignored if the peer is already known to
		// have the inventory.
		sp.QueueInventory(msg.invVect)
	}
}

// handleBroadcastMsg deals with broadcasting messages to peers.  It is invoked
// from the peerHandler goroutine.
func (s *server) handleBroadcastMsg(state *peerState, bmsg *broadcastMsg) {
	sps := make([]*serverPeer, 0)

	state.ForAllPeers(func(sp *serverPeer) {
		if !sp.Connected() {
			return
		}

		for _, ep := range bmsg.excludePeers {
			if sp == ep {
				return
			}
		}

		sps = append(sps, sp)
	})
	for _, sp := range sps {
		sp.QueueMessage(bmsg.message, nil)
	}
}

type getConnCountMsg struct {
	contype byte // type: 0 - all, 1 - inbound, 2 - outbound
	reply   chan int32
}

type getPeersMsg struct {
	reply chan []*serverPeer
}

type getOutboundGroup struct {
	key   string
	reply chan int
}

type getAddedNodesMsg struct {
	reply chan []*serverPeer
}

type disconnectNodeMsg struct {
	cmp   func(*serverPeer) bool
	reply chan error
}

type connectNodeMsg struct {
	addr      string
	permanent bool
	reply     chan error
}

type removeNodeMsg struct {
	cmp   func(*serverPeer) bool
	reply chan error
}

// handleQuery is the central handler for all queries and commands from other
// goroutines related to peer state.
func (s *server) handleQuery(state *peerState, querymsg interface{}) {
	switch msg := querymsg.(type) {
	case getConnCountMsg:
		nconnected := int32(0)
		switch msg.contype {
		case 0:
			state.ForAllPeers(func(sp *serverPeer) {
				if sp.Connected() {
					nconnected++
				}
			})
		case 1:
			state.ForAllPeers(func(sp *serverPeer) {
				if sp.Connected() {
					nconnected++
				}
			})
			state.ForAllOutboundPeers(func(sp *serverPeer) {
				if sp.Connected() {
					nconnected--
				}
			})
		case 2:
			state.ForAllOutboundPeers(func(sp *serverPeer) {
				if sp.Connected() {
					nconnected++
				}
			})
		}
		msg.reply <- nconnected

	case getPeersMsg:
		peers := make([]*serverPeer, 0, state.Count(7))
		state.ForAllPeers(func(sp *serverPeer) {
			if !sp.Connected() {
				return
			}
			peers = append(peers, sp)
		})
		msg.reply <- peers

	case connectNodeMsg:
		// TODO: duplicate oneshots?
		// Limit max number of total peers.
		if state.Count(6) >= s.prot.cfg.MaxPeers {
			msg.reply <- errors.New("max peers reached")
			return
		}

		state.cmutex.Lock()
		for _, peer := range state.persistentPeers {
			if peer.Addr() == msg.addr {
				var err error
				if msg.permanent {
					err = errors.New("peer already connected")
				} else {
					err = errors.New("peer exists as a permanent peer")
				}
				state.cmutex.Unlock()
				msg.reply <- err
				return
			}
		}
		state.cmutex.Unlock()

		if !msg.permanent && len(state.persistentPeers) > 0 {
			msg.reply <- nil
			return
		}

		netAddr, err := addrStringToNetAddr(msg.addr, s.prot.cfg)
		if err != nil {
			msg.reply <- err
			return
		}

		// TODO: if too many, nuke a non-perm peer.
		go s.connManager.Connect(&connmgr.ConnReq{
			Addr:      netAddr,
			Permanent: msg.permanent,
		})
		msg.reply <- nil
	case removeNodeMsg:
		state.cmutex.Lock()
		found := disconnectPeer(state.persistentPeers, msg.cmp, func(sp *serverPeer) {
			// Keep group counts ok since we remove from
			// the list now.
			state.outboundGroups[addrmgr.GroupKey(sp.NA())]--
		})
		state.cmutex.Unlock()

		if found {
			msg.reply <- nil
		} else {
			msg.reply <- errors.New("peer not found")
		}
	case getOutboundGroup:
		count, ok := state.outboundGroups[msg.key]
		if ok {
			msg.reply <- count
		} else {
			msg.reply <- 0
		}
	// Request a list of the persistent (added) peers.
	case getAddedNodesMsg:
		// Respond with a slice of the relevant peers.
		state.cmutex.Lock()
		peers := make([]*serverPeer, 0, len(state.persistentPeers))
		for _, sp := range state.persistentPeers {
			peers = append(peers, sp)
		}
		state.cmutex.Unlock()
		msg.reply <- peers
	case disconnectNodeMsg:
		// Check inbound peers. We pass a nil callback since we don't
		// require any additional actions on disconnect for inbound peers.
		//		btcdLog.Infof("cmutex.Lock @ disconnectNodeMsg")
		state.cmutex.Lock()
		found := disconnectPeer(state.inboundPeers, msg.cmp, nil)
		//		btcdLog.Infof("cmutex.Unlock")
		state.cmutex.Unlock()
		if found {
			msg.reply <- nil
			return
		}

		// Check outbound peers.
		found = true
		real := false

		// If there are multiple outbound connections to the same
		// ip:port, continue disconnecting them all until no such
		// peers are found.
		for found {
			//				btcdLog.Infof("cmutex.Lock @ disconnectNodeMsg")
			state.cmutex.Lock()
			found = disconnectPeer(state.outboundPeers, msg.cmp, func(sp *serverPeer) {
				state.outboundGroups[addrmgr.GroupKey(sp.NA())]--
			})
			real = real || found
			//				btcdLog.Infof("cmutex.Unlock")
			state.cmutex.Unlock()
		}

		if real {
			msg.reply <- nil
			return
		}

		msg.reply <- errors.New("peer not found")
	}
}

// disconnectPeer attempts to drop the connection of a targeted peer in the
// passed peer list. Targets are identified via usage of the passed
// `compareFunc`, which should return `true` if the passed peer is the target
// peer. This function returns true on success and false if the peer is unable
// to be located. If the peer is found, and the passed callback: `whenFound'
// isn't nil, we call it with the peer as the argument before it is removed
// from the peerList, and is disconnected from the server.
func disconnectPeer(peerList map[int32]*serverPeer, compareFunc func(*serverPeer) bool, whenFound func(*serverPeer)) bool {
	for addr, peer := range peerList {
		if compareFunc(peer) {
			if whenFound != nil {
				whenFound(peer)
			}

			// This is ok because we are not continuing
			// to iterate so won't corrupt the loop.
			delete(peerList, addr)
			peer.Disconnect("disconnectPeer")
			return true
		}
	}
	return false
}

// newPeerConfig returns the configuration for the given serverPeer.
func newPeerConfig(sp *serverPeer, svp bool) *peer.Config {
	return &peer.Config{
		Listeners: peer.MessageListeners{
			OnVersion:      sp.OnVersion,
			OnMemPool:      sp.OnMemPool,
			OnTx:           sp.OnTx,
			OnBlock:        sp.OnBlock,
			OnMinerBlock:   sp.OnMinerBlock,
			OnInv:          sp.OnInv,
			OnHeaders:      sp.OnHeaders,
			OnGetData:      sp.OnGetData,
			OnGetBlocks:    sp.OnGetBlocks,
			OnGetHeaders:   sp.OnGetHeaders,
			OnGetCFilters:  nil, // sp.OnGetCFilters,
			OnGetCFHeaders: nil, // sp.OnGetCFHeaders,
			OnGetCFCheckpt: nil, // sp.OnGetCFCheckpt,
			OnFeeFilter:    nil, // sp.OnFeeFilter,
			OnFilterAdd:    nil, // sp.OnFilterAdd,
			OnFilterClear:  nil, // sp.OnFilterClear,
			OnFilterLoad:   nil, // sp.OnFilterLoad,
			OnGetAddr:      sp.OnGetAddr,
			OnAddr:         sp.OnAddr,
			OnRead:         sp.OnRead,
			OnWrite:        sp.OnWrite,
			PushGetBlock:   sp.PushGetBlock,
			OnReject:       sp.OnReject,
			OnAlert:        sp.OnAlert,
			OnSignatures:   sp.OnSignatures,

			OnFinalized: sp.OnFinalized,
			OnFinal:     sp.OnFinal,
			//OnGetChainMap: sp.OnGetChainMap,
			//OnChainMap:    sp.OnChainMap,
		},
		NewestBlock:       sp.newestBlock,
		NewestMinerBlock:  sp.newestMinerBlock,
		HostToNetAddress:  sp.server.addrManager.HostToNetAddress,
		Proxy:             sp.server.prot.cfg.Proxy,
		UserAgentName:     userAgentName,
		UserAgentVersion:  userAgentVersion,
		UserAgentComments: sp.server.prot.cfg.UserAgentComments,
		ChainParams:       sp.server.chainParams,
		Services:          sp.server.services,
		DisableRelayTx:    sp.server.prot.cfg.BlocksOnly,
		ProtocolVersion:   peer.MaxProtocolVersion,
		TrickleInterval:   sp.server.prot.cfg.TrickleInterval,
		IsSvp:             svp,
	}
}

func (s *server) ResetConnections() {
	s.syncManager.ResetConnections(false)
}

// inboundPeerConnected is invoked by the connection manager when a new inbound
// connection is established.  It initializes a new inbound server peer
// instance, associates it with the connection, and starts a goroutine to wait
// for disconnection.
func (s *server) inboundPeerConnected(conn net.Conn) {
	/*
		// check if we will accept this conn. there is a max limit of 5 conn./peer host
		// temp fix for too many conn from one address. we should fix from the other end:
		// not to initiate conn at the first place
		n := 0
		t := strings.Split(conn.RemoteAddr().String(), ":")
		for _, p := range s.peerState.inboundPeers {
			s := strings.Split(p.Addr(), ":")
			if s[0] == t[0] {
				n++
			}
		}
		if n >= 5 {
			srvrLog.Infof("Reject connection from %s because too many from the host", t[0])
			conn.Close()
			return
		}
	*/

	sp := newServerPeer(s, false)
	sp.isWhitelisted = isWhitelisted(conn.RemoteAddr(), s.prot.cfg)
	sp.Peer = peer.NewInboundPeer(newPeerConfig(sp, s.chain.IsSVP))
	sp.AssociateConnection(conn)
	go s.peerDoneHandler(sp)
}

func (s *server) outboundPeerDisConnected(c *connmgr.ConnReq) {
}

// outboundPeerConnected is invoked by the connection manager when a new
// outbound connection is established.  It initializes a new outbound server
// peer instance, associates it with the relevant state such as the connection
// request instance and the connection itself, and finally notifies the address
// manager of the attempt.
func (s *server) outboundPeerConnected(c *connmgr.ConnReq, conn net.Conn) {
	sp := newServerPeer(s, c.Permanent)
	p, err := peer.NewOutboundPeer(newPeerConfig(sp, s.prot.IsSvp), c.Addr.String())
	if err != nil {
		srvrLog.Debugf("Cannot create outbound peer %s: %v", c.Addr, err)
		s.connManager.Disconnect(c.ID())
	}
	sp.Peer = p
	sp.connReq = c
	sp.isWhitelisted = isWhitelisted(conn.RemoteAddr(), s.prot.cfg)
	sp.AssociateConnection(conn)
	go s.peerDoneHandler(sp)
	s.addrManager.Attempt(sp.NA())
}

func (s *server) Broadcast(m wire.Message, ps *string) {
	s.syncManager.Broadcast(m, ps)
}

func (s *server) Randcast(m wire.Message, ps *string) {
	s.syncManager.Randcast(m, ps)
}

// peerDoneHandler handles peer disconnects by notifiying the server that it's
// done along with other performing other desirable cleanup.
func (s *server) peerDoneHandler(sp *serverPeer) {
	sp.WaitForDisconnect()
	s.donePeers <- sp

	// Only tell sync manager we are gone if we ever told it we existed.
	if sp.VersionKnown() {
		s.syncManager.DonePeer(sp.Peer)

		// Evict any remaining orphans that were sent by the peer.
		numEvicted := s.txMemPool.RemoveOrphansByTag(mempool.Tag(sp.ID()))
		if numEvicted > 0 {
			txmpLog.Infof("Evicted %d %s from peer %v (id %d)",
				numEvicted, pickNoun(numEvicted, "orphan",
					"orphans"), sp, sp.ID())
		}
	}
	close(sp.quit)
}

// peerHandler is used to handle peer operations such as adding and removing
// peers to and from the server, banning peers, and broadcasting messages to
// peers.  It must be run in a goroutine.
func (s *server) peerHandler() {
	// Start the address manager and sync manager, both of which are needed
	// by peers.  This is done here since their lifecycle is closely tied
	// to this handler and rather than adding more channels to sychronize
	// things, it's easier and slightly faster to simply start and stop them
	// in this handler.
	s.addrManager.Start()
	if s.syncManager != nil {
		s.syncManager.Start()
	}

	srvrLog.Tracef("Starting peer handler")

	state := &peerState{
		connManager:     s.connManager,
		inboundPeers:    make(map[int32]*serverPeer),
		persistentPeers: make(map[int32]*serverPeer),
		outboundPeers:   make(map[int32]*serverPeer),
		banned:          make(map[string]time.Time),
		outboundGroups:  make(map[string]int),
		committee:       make(map[[20]byte]*committeeState),
	}

	s.peerState = state

	wrapbtcdLookup := func(host string) ([]net.IP, error) {
		return btcdLookup(host, s.prot.cfg)
	}

	if !s.prot.cfg.DisableDNSSeed {
		// Add peers discovered through DNS to the address manager.
		connmgr.SeedFromDNS(s.chainParams, defaultRequiredServices,
			wrapbtcdLookup, func(addrs []*wire.NetAddress) {
				// Omega uses a lookup of the dns seeder here. This
				// is rather strange since the values looked up by the
				// DNS seed lookups will vary quite a lot.
				// to replicate this behaviour we put all addresses as
				// having come from the first one.
				s.addrManager.AddAddresses(addrs, addrs[0])
			})
	}
	go s.connManager.Start(s.peerState)

	newBlock := make(chan int32, 50)

	if s.chain != nil {
		s.chain.Subscribe(func(msg *blockchain.Notification) {
			if msg.Type == blockchain.NTBlockConnected {
				s.connManager.Alive = time.Now()

				if msg.Data == nil {
					return
				}

				block := msg.Data.(*btcutil.Block)
				if block == nil {
					return
				}
				nonce := block.MsgBlock().Header.Nonce
				if nonce <= -wire.MINER_RORATE_FREQ || nonce > 0 {
					newBlock <- int32(s.chain.BestSnapshot().LastRotation)
				}
			}
		})
		// initialize committee
		newBlock <- int32(s.chain.BestSnapshot().LastRotation)
	}

out:
	for {
		select {
		case <-s.quit:
			btcdLog.Tracef("peerHandler: quit")
			// Disconnect all peers on server shutdown.
			state.ForAllPeers(func(sp *serverPeer) {
				srvrLog.Tracef("Shutdown peer %s", sp)
				sp.Disconnect("peerHandler @ quit")
			})
			btcdLog.Tracef("peerHandler: quit - done")

			break out

		case r := <-newBlock:
			btcdLog.Tracef("peerHandler: newBlock - handleCommitteRotation")
			s.handleCommitteRotation(r)
			btcdLog.Tracef("peerHandler: newBlock - handleCommitteRotation - done")

		// New peers connected to the server.
		case p := <-s.newPeers:
			btcdLog.Tracef("peerHandler: newPeers - handleAddPeerMsg")
			s.handleAddPeerMsg(state, p)
			btcdLog.Tracef("peerHandler: newPeers - handleAddPeerMsg - done")

		// Disconnected peers.
		case p := <-s.donePeers:
			btcdLog.Tracef("peerHandler: donePeers - handleDonePeerMsg")
			s.handleDonePeerMsg(state, p)
			btcdLog.Tracef("peerHandler: donePeers - handleDonePeerMsg - done")

		// Block accepted in mainchain or orphan, update peer height.
		case umsg := <-s.peerHeightsUpdate:
			btcdLog.Tracef("peerHeightsUpdate: newBlock - handleUpdatePeerHeights")
			s.handleUpdatePeerHeights(state, umsg)
			btcdLog.Tracef("peerHeightsUpdate: newBlock - handleUpdatePeerHeights - done")

		case umsg := <-s.peerMinerHeightsUpdate:
			btcdLog.Tracef("peerHandler: peerMinerHeightsUpdate - handleUpdatePeerMinerHeights")
			s.handleUpdatePeerMinerHeights(state, umsg)
			btcdLog.Tracef("peerHandler: peerMinerHeightsUpdate - handleUpdatePeerMinerHeights - done")

		// Peer to ban.
		case p := <-s.banPeers:
			btcdLog.Tracef("peerHandler: banPeers - handleBanPeerMsg")
			s.handleBanPeerMsg(state, p)
			btcdLog.Tracef("peerHandler: banPeers - handleBanPeerMsg - done")

		// New inventory to potentially be relayed to other peers.
		case invMsg := <-s.relayInv:
			btcdLog.Tracef("peerHandler: relayInv - handleRelayInvMsg")
			s.handleRelayInvMsg(state, invMsg)
			btcdLog.Tracef("peerHandler: relayInv - handleRelayInvMsg - done")

		// Message to broadcast to all connected peers except those
		// which are excluded by the message.
		case bmsg := <-s.broadcast:
			btcdLog.Tracef("peerHandler: broadcast - handleBroadcastMsg")
			s.handleBroadcastMsg(state, &bmsg)
			btcdLog.Tracef("peerHandler: broadcast - handleBroadcastMsg - done")

		case qmsg := <-s.query:
			btcdLog.Tracef("peerHandler: query - handleQuery")
			s.handleQuery(state, qmsg)
			btcdLog.Tracef("peerHandler: query - handleQuery - done")
		}
	}

	btcdLog.Infof("Stopping connManager")
	s.connManager.Stop()
	btcdLog.Infof("connManager stopped")

	if s.syncManager != nil {
		btcdLog.Infof("Stopping syncManager")
		s.syncManager.Stop()
		btcdLog.Infof("syncManager stopped")
	}

	btcdLog.Infof("Stopping addrManager")
	s.addrManager.Stop()
	btcdLog.Infof("addrManager stopped")

	btcdLog.Tracef("All Peer handler go routines shut down")

	// Drain channels before exiting so nothing is left waiting around
	// to send.
cleanup:
	for {
		select {
		case <-s.newPeers:
		case <-s.donePeers:
		case <-s.peerHeightsUpdate:
		case <-s.peerMinerHeightsUpdate:
		case <-s.relayInv:
		case <-s.broadcast:
		case <-s.query:
		default:
			break cleanup
		}
	}
	s.wg.Done()
	srvrLog.Tracef("Peer handler done")
}

// AddPeer adds a new peer that has already been connected to the server.
func (s *server) AddPeer(sp *serverPeer) {
	s.newPeers <- sp
}

// BanPeer bans a peer that has already been connected to the server by ip.
func (s *server) BanPeer(sp *serverPeer) {
	s.banPeers <- sp
}

// RelayInventory relays the passed inventory vector to all connected peers
// that are not already known to have it.
func (s *server) RelayInventory(invVect *wire.InvVect, data interface{}) {
	s.relayInv <- relayMsg{invVect: invVect, data: data}
}

// BroadcastMessage sends msg to all peers currently connected to the server
// except those in the passed peers to exclude.
func (s *server) BroadcastMessage(msg wire.Message, check bool, exclPeers ...*serverPeer) {
	// XXX: Need to determine if this is an alert that has already been
	// broadcast and refrain from broadcasting again.
	var h chainhash.Hash
	if check {
		var w bytes.Buffer
		msg.OmcEncode(&w, 0, 0)
		h = chainhash.DoubleHashH(w.Bytes())
	}

	if t, ok := s.Broadcasted[h]; !check || !ok {
		if check {
			s.Broadcasted[h] = time.Now().Unix()
		}
		bmsg := broadcastMsg{message: msg, excludePeers: exclPeers}
		s.broadcast <- bmsg
	} else if time.Now().Unix()-t > 300 {
		// msg expired
		delete(s.Broadcasted, h)
	} // don't rebroadcast in 5 minutes
}

// ConnectedCount returns the number of currently connected peers.
func (s *server) ConnectedCount(contype byte) int32 {
	replyChan := make(chan int32)

	s.query <- getConnCountMsg{contype: contype, reply: replyChan}

	return <-replyChan
}

// OutboundGroupCount returns the number of peers connected to the given
// outbound group key.
func (s *server) OutboundGroupCount(key string) int {
	replyChan := make(chan int)
	s.query <- getOutboundGroup{key: key, reply: replyChan}
	return <-replyChan
}

// AddBytesSent adds the passed number of bytes to the total bytes sent counter
// for the server.  It is safe for concurrent access.
func (s *server) AddBytesSent(bytesSent uint64) {
	atomic.AddUint64(&s.bytesSent, bytesSent)
}

// AddBytesReceived adds the passed number of bytes to the total bytes received
// counter for the server.  It is safe for concurrent access.
func (s *server) AddBytesReceived(bytesReceived uint64) {
	atomic.AddUint64(&s.bytesReceived, bytesReceived)
}

// NetTotals returns the sum of all bytes received and sent across the network
// for all peers.  It is safe for concurrent access.
func (s *server) NetTotals() (uint64, uint64) {
	return atomic.LoadUint64(&s.bytesReceived),
		atomic.LoadUint64(&s.bytesSent)
}

// UpdatePeerHeights updates the heights of all peers who have have announced
// the latest connected main chain block, or a recognized orphan. These height
// updates allow us to dynamically refresh peer heights, ensuring sync peer
// selection has access to the latest block heights for each peer.
func (s *server) UpdatePeerHeights(latestBlkHash *chainhash.Hash, latestHeight int32, updateSource *peer.Peer) {
	s.peerHeightsUpdate <- updatePeerHeightsMsg{
		newHash:    latestBlkHash,
		newHeight:  latestHeight,
		originPeer: updateSource,
	}
	//	consensus.UpdateChainHeight(s.chain.BestSnapshot().Height)
}

func (s *server) UpdatePeerMinerHeights(latestBlkHash *chainhash.Hash, latestHeight int32, updateSource *peer.Peer) {
	s.peerMinerHeightsUpdate <- updatePeerHeightsMsg{
		newHash:    latestBlkHash,
		newHeight:  latestHeight,
		originPeer: updateSource,
	}
}

// rebroadcastHandler keeps track of user submitted inventories that we have
// sent out but have not yet made it into a block. We periodically rebroadcast
// them in case our peers restarted or otherwise lost track of them.
func (s *server) rebroadcastHandler() {
	// Wait 5 min before first tx rebroadcast.
	timer := time.NewTimer(5 * time.Minute)
	pendingInvs := make(map[wire.InvVect]interface{})

out:
	for {
		select {
		case <-s.quit:
			break out

		case riv := <-s.modifyRebroadcastInv:
			switch msg := riv.(type) {
			// Incoming InvVects are added to our map of RPC txs.
			case broadcastInventoryAdd:
				pendingInvs[*msg.invVect] = msg.data

			// When an InvVect has been added to a block, we can
			// now remove it, if it was present.
			case broadcastInventoryDel:
				if _, ok := pendingInvs[*msg]; ok {
					delete(pendingInvs, *msg)
				}
			}

		case <-timer.C:
			// Any inventory we have has not made it into a block
			// yet. We periodically resubmit them until they have.
			for iv, data := range pendingInvs {
				ivCopy := iv
				s.RelayInventory(&ivCopy, data)
			}

			// Process at a random time up to 30mins (in seconds)
			// in the future.
			timer.Reset(time.Second * time.Duration(randomUint16Number(1800)))
		}
	}

	timer.Stop()

	// Drain channels before exiting so nothing is left waiting around
	// to send.
cleanup:
	for {
		select {
		case <-s.modifyRebroadcastInv:
		default:
			break cleanup
		}
	}
	s.wg.Done()
	srvrLog.Tracef("rebroadcastHandler done")
}

// Start begins accepting connections from peers.
func (s *server) Start() {
	// Already started?
	if atomic.AddInt32(&s.started, 1) != 1 {
		return
	}

	srvrLog.Trace("Starting server")

	// Server startup time. Used for the uptime command for uptime calculation.
	s.startupTime = time.Now().Unix()

	// Start the peer handler which in turn starts the address and block
	// managers.
	s.wg.Add(1)
	go s.peerHandler()

	if s.nat != nil {
		s.wg.Add(1)
		go s.upnpUpdateThread()
	}

	if s.rpcServer != nil && !s.rpcServer.cfg.Cfg.DisableRPC {
		s.wg.Add(1)

		// Start the rebroadcastHandler, which ensures user tx received by
		// the RPC server are rebroadcast until being included in a block.
		go s.rebroadcastHandler()

		s.rpcServer.Start()
	}

	// Start the CPU miner if generation is enabled.
	//	if cfg.Generate {
	btcdLog.Infof("Start minging blocks.")
	if s.cpuMiner != nil && (s.prot.cfg.Generate || !s.prot.cfg.DisablePOWMining) {
		s.cpuMiner.Start()
	}
	//	}
	if s.prot.cfg.GenerateMiner {
		btcdLog.Infof("Start minging miner blocks.")
		s.minerMiner.Start()
	}
}

// Stop gracefully shuts down the server by stopping and disconnecting all
// peers and the main listener.
func (s *server) Stop() error {
	btcdLog.Info("Server Stop")
	// Make sure this only happens once.
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		srvrLog.Infof("Server is already in the process of shutting down")
		return nil
	}

	btcdLog.Info("Server shutting down")
	srvrLog.Warnf("Server shutting down")

	if s.minerMiner != nil {
		btcdLog.Info("Server minerMiner Stop")
		s.minerMiner.Stop()
	}

	// Stop the CPU miner if needed
	if s.cpuMiner != nil {
		btcdLog.Info("Server cpuMiner Stop")
		s.cpuMiner.Stop()
	}

	// Shutdown the RPC server if it's not disabled.
	if s.rpcServer != nil && !s.rpcServer.cfg.Cfg.DisableRPC {
		btcdLog.Info("Server rpcServer Stop")
		s.rpcServer.Stop()
	}
	btcdLog.Info("Save fee estimator state in the database")

	// Save fee estimator state in the database.
	if !s.prot.cfg.Passive {
		s.db.Update(func(tx database.Tx) error {
			metadata := tx.Metadata()
			metadata.Put(mempool.EstimateFeeDatabaseKey, s.feeEstimator.Save())

			return nil
		})
	}

	// Signal the remaining goroutines to quit.
	btcdLog.Info("Signal the remaining goroutines to quit")

	close(s.quit)
	return nil
}

// WaitForShutdown blocks until the main listener and peer handlers are stopped.
func (s *server) WaitForShutdown() {
	s.wg.Wait()
}

// ScheduleShutdown schedules a server shutdown after the specified duration.
// It also dynamically adjusts how often to warn the server is going down based
// on remaining duration.
func (s *server) ScheduleShutdown(duration time.Duration) {
	// Don't schedule shutdown more than once.
	if atomic.AddInt32(&s.shutdownSched, 1) != 1 {
		return
	}
	srvrLog.Warnf("Server shutdown in %v", duration)
	go func() {
		remaining := duration
		tickDuration := dynamicTickDuration(remaining)
		done := time.After(remaining)
		ticker := time.NewTicker(tickDuration)
	out:
		for {
			select {
			case <-done:
				ticker.Stop()
				s.Stop()
				break out
			case <-ticker.C:
				remaining = remaining - tickDuration
				if remaining < time.Second {
					continue
				}

				// Change tick duration dynamically based on remaining time.
				newDuration := dynamicTickDuration(remaining)
				if tickDuration != newDuration {
					tickDuration = newDuration
					ticker.Stop()
					ticker = time.NewTicker(tickDuration)
				}
				srvrLog.Warnf("Server shutdown in %v", remaining)
			}
		}
	}()
}

// parseListeners determines whether each listen address is IPv4 and IPv6 and
// returns a slice of appropriate net.Addrs to listen on with TCP. It also
// properly detects addresses which apply to "all interfaces" and adds the
// address as both IPv4 and IPv6.
func parseListeners(addrs []string) ([]net.Addr, error) {
	netAddrs := make([]net.Addr, 0, len(addrs)*2)
	for _, addr := range addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// Shouldn't happen due to already being normalized.
			return nil, err
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}

		// Empty host or host of * on plan9 is both IPv4 and IPv6.
		if host == "" || (host == "*" && runtime.GOOS == "plan9") {
			netAddrs = append(netAddrs, simpleAddr{net: "tcp4", addr: addr})
			netAddrs = append(netAddrs, simpleAddr{net: "tcp6", addr: addr})
			continue
		}

		// Strip IPv6 zone id if present since net.ParseIP does not
		// handle it.
		zoneIndex := strings.LastIndex(host, "%")
		if zoneIndex > 0 {
			host = host[:zoneIndex]
		}

		// Parse the IP.
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("'%s' is not a valid IP address", host)
		}

		// To4 returns nil when the IP is not an IPv4 address, so use
		// this determine the address type.
		if ip.To4() == nil {
			netAddrs = append(netAddrs, simpleAddr{net: "tcp6", addr: addr})
		} else {
			netAddrs = append(netAddrs, simpleAddr{net: "tcp4", addr: addr})
		}
	}
	return netAddrs, nil
}

func (s *server) GetDefinition(hashes map[chainhash.Hash]uint8) {
	// find a full client peer s connects to, if none, pick any peer
	// send a request to this one
}

func (s *server) upnpUpdateThread() {
	// Go off immediately to prevent code duplication, thereafter we renew
	// lease every 15 minutes.
	timer := time.NewTimer(0 * time.Second)
	lport, _ := strconv.ParseInt(s.chainParams.DefaultPort, 10, 16)
	first := true
out:
	for {
		select {
		case <-s.quit:
			break out

		case <-timer.C:
			// TODO: pick external port  more cleverly
			// TODO: know which ports we are listening to on an external net.
			// TODO: if specific listen port doesn't work then ask for wildcard
			// listen port?
			// XXX this assumes timeout is in seconds.
			listenPort, err := s.nat.AddPortMapping("tcp", int(lport), int(lport),
				"btcd listen port", 20*60)
			if err != nil {
				srvrLog.Warnf("can't add UPnP port mapping: %v", err)
			}
			if first && err == nil {
				// TODO: look this up periodically to see if upnp domain changed
				// and so did ip.
				externalip, err := s.nat.GetExternalAddress()
				if err != nil {
					srvrLog.Warnf("UPnP can't get external address: %v", err)
					continue out
				}
				na := wire.NewNetAddressIPPort(externalip, uint16(listenPort),
					s.services)
				_, err = s.addrManager.AddLocalAddress(na, addrmgr.UpnpPrio)
				if err != nil {
					// XXX DeletePortMapping?
				}
				srvrLog.Warnf("Successfully bound via UPnP to %s", addrmgr.NetAddressKey(na))
				first = false
			}
			timer.Reset(time.Minute * 15)
		}
	}

	timer.Stop()

	if err := s.nat.DeletePortMapping("tcp", int(lport), int(lport)); err != nil {
		srvrLog.Warnf("unable to remove UPnP port mapping: %v", err)
	} else {
		srvrLog.Debugf("successfully disestablished UPnP port mapping")
	}

	s.wg.Done()
	srvrLog.Tracef("upnpUpdateThread done")
}

// setupRPCListeners returns a slice of listeners that are configured for use
// with the RPC server depending on the configuration settings for listen
// addresses and TLS.
func setupRPCListeners(cfg *config) ([]net.Listener, error) {
	// Setup TLS if not disabled.
	listenFunc := net.Listen
	if !cfg.DisableTLS {
		// Generate the TLS cert and key file if both don't already
		// exist.
		if !fileExists(cfg.RPCKey) && !fileExists(cfg.RPCCert) {
			err := genCertPair(cfg.RPCCert, cfg.RPCKey)
			if err != nil {
				return nil, err
			}
		}
		keypair, err := tls.LoadX509KeyPair(cfg.RPCCert, cfg.RPCKey)
		if err != nil {
			return nil, err
		}

		tlsConfig := tls.Config{
			Certificates: []tls.Certificate{keypair},
			MinVersion:   tls.VersionTLS12,
		}

		// Change the standard net.Listen function to the tls one.
		listenFunc = func(net string, laddr string) (net.Listener, error) {
			return tls.Listen(net, laddr, &tlsConfig)
		}
	}

	netAddrs, err := parseListeners(cfg.RPCListeners)
	if err != nil {
		return nil, err
	}

	listeners := make([]net.Listener, 0, len(netAddrs))
	for _, addr := range netAddrs {
		listener, err := listenFunc(addr.Network(), addr.String())
		if err != nil {
			rpcsLog.Warnf("Can't listen on %s: %v", addr, err)
			continue
		}
		listeners = append(listeners, listener)
	}

	return listeners, nil
}

// newServer returns a new btcd server configured to listen on addr for the
// bitcoin network type specified by chainParams.  Use start to begin accepting
// connections from peers.
func newServer(listenAddrs []string, db, minerdb database.DB, prot *Protocol, interrupt <-chan struct{}) (*server, error) {
	services := defaultServices
	if prot.cfg.NoPeerBloomFilters {
		services &^= common.SFNodeBloom
	}

	wrapbtcdLookup := func(host string) ([]net.IP, error) {
		return btcdLookup(host, prot.cfg)
	}

	amgr := addrmgr.New(prot.cfg.DataDir, wrapbtcdLookup, prot.cfg.ExternalIPs)

	var listeners []net.Listener
	var nat NAT
	if !prot.cfg.DisableListen {
		var err error
		srvrLog.Info("Listening at ", listenAddrs, services)
		listeners, nat, err = initListeners(amgr, listenAddrs, services, prot)
		if err != nil {
			srvrLog.Info(". Failed")
			return nil, err
		}
		if len(listeners) == 0 {
			srvrLog.Info("no valid listen address")
			return nil, errors.New("no valid listen address")
		}
		srvrLog.Info("OK")
	}

	minerdb.View(func(dbTx database.Tx) error {
		if prot.IsSvp {
			return nil
		}
		bucket := dbTx.Metadata().Bucket([]byte(common.MiningKeys))

		if bucket == nil {
			return nil
		}

		cursor := bucket.Cursor()
		for ok := cursor.First(); ok; ok = cursor.Next() {
			dwif, err := btcutil.DecodeWIF(string(cursor.Key()))

			fmt.Printf("Mining Key: %s\n", string(cursor.Key()))
			if err == nil {
				pkaddr, _ := btcutil.NewAddressPubKey(dwif.PrivKey.PubKey().SerializeCompressed(), activeNetParams)
				addr := pkaddr.AddressPubKeyHash()

				matched := false
				for _, adr := range prot.cfg.signAddress {
					matched = matched || adr.String() == addr.String()
				}
				if matched {
					continue
				}

				prot.cfg.privateKeys = append(prot.cfg.privateKeys, dwif.PrivKey)
				prot.cfg.signAddress = append(prot.cfg.signAddress, addr)
				fmt.Printf("signAddress: %s\n", addr.EncodeAddress())
			}
		}
		return nil
	})

	if !prot.IsSvp && common.Licensed && len(prot.cfg.privateKeys) == 0 {
		// get it from license server
		url := "http://omegasuite.org:6600/setup?serialno=" + string(SerialNo) + "&activated=" + strconv.FormatInt(ActivateTime, 10)
		// url := "http://localhost:6600/setup?serialno=" + string(SerialNo) + "&activated=" + strconv.FormatInt(ActivateTime, 10)

		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("Can not reach license servr")
			os.Exit(0)
		}
		body, err := ioutil.ReadAll(resp.Body)

		// JSON decode
		var msg struct {
			Status int
			Error  struct {
				Message string
			}
		}

		// 读取响应体内容
		if err == nil {
			resp.Body.Close() // 确保在函数结束时关闭响应体

			json.Unmarshal(body, &msg)
			if msg.Status <= 0 {
				fmt.Printf("License denied")
				os.Exit(0)
			}
		} else {
			fmt.Printf("License denied")
			os.Exit(0)
		}

		if minerdb.Update(func(dbTx database.Tx) error {
			bucket := dbTx.Metadata().Bucket([]byte(common.MiningKeys))

			if bucket == nil {
				if _, err = dbTx.Metadata().CreateBucket([]byte(common.MiningKeys)); err != nil {
					return fmt.Errorf("Missing MiningKeys table")
				}
				bucket = dbTx.Metadata().Bucket([]byte(common.MiningKeys))
			}

			dwif, err := btcutil.DecodeWIF(msg.Error.Message)
			if err != nil {
				return err
			}
			bucket.Put([]byte(msg.Error.Message), []byte{1})
			pkaddr, err := btcutil.NewAddressPubKey(dwif.PrivKey.PubKey().SerializeCompressed(), activeNetParams)
			if err != nil {
				return err
			}

			addr := pkaddr.AddressPubKeyHash()

			prot.cfg.privateKeys = append(prot.cfg.privateKeys, dwif.PrivKey)
			prot.cfg.signAddress = append(prot.cfg.signAddress, addr)
			fmt.Printf("signAddress: %s\n", addr.EncodeAddress())

			return nil
		}) != nil {
			fmt.Printf("License denied")
			os.Exit(0)
		}
	}

	if !prot.IsSvp && common.Licensed && ActivateTime != 0 {
		// get it from license server
		url := "http://omegasuite.org:6600/getcollateral?serialno=" + string(SerialNo) + "&activated=" + strconv.FormatInt(ActivateTime, 10)
		// url := "http://localhost:6600/getcollateral?serialno=" + string(SerialNo) + "&activated=" + strconv.FormatInt(ActivateTime, 10)

		resp, err := http.Get(url)

		hascoll := false
		var msg struct {
			Status int
			Error  struct {
				Message string
			}
		}

		if err == nil {
			body, err := ioutil.ReadAll(resp.Body)

			// JSON decode

			// 读取响应体内容
			if err == nil {
				resp.Body.Close() // 确保在函数结束时关闭响应体

				json.Unmarshal(body, &msg)
				if msg.Status != 0 && len(msg.Error.Message) > 0 {
					hascoll = true
				}
			}
		}

		if hascoll {
			txs := make([]string, 0)
			if len(msg.Error.Message) > 0 {
				json.Unmarshal([]byte(msg.Error.Message), &txs)
				if len(txs) > 0 {
					minerdb.Update(func(dbTx database.Tx) error {
						bucket := dbTx.Metadata().Bucket([]byte(common.MiningCollaterals))
						if bucket == nil {
							if _, err = dbTx.Metadata().CreateBucket([]byte(common.MiningCollaterals)); err != nil {
								return fmt.Errorf("Missing MiningCollaterals table")
							}
							bucket = dbTx.Metadata().Bucket([]byte(common.MiningCollaterals))
						}

						for _, tx := range txs {
							utxo := strings.Split(tx, ":")
							if len(utxo) != 2 {
								continue
							}

							txid, _ := hex.DecodeString(utxo[0])
							if len(txid) != 32 {
								continue
							}
							var key [36]byte
							copy(key[:], txid[:])

							index, _ := strconv.Atoi(utxo[1])

							common.LittleEndian.PutUint32(key[32:], uint32(index))
							bucket.Put(key[:], []byte{1})
						}

						return nil
					})
				}
			}
		}
	}

	s := server{
		chainParams:            prot.activeNetParams,
		addrManager:            amgr,
		newPeers:               make(chan *serverPeer, prot.cfg.MaxPeers),
		donePeers:              make(chan *serverPeer, prot.cfg.MaxPeers),
		banPeers:               make(chan *serverPeer, prot.cfg.MaxPeers),
		query:                  make(chan interface{}, 1000),
		relayInv:               make(chan relayMsg, prot.cfg.MaxPeers),
		broadcast:              make(chan broadcastMsg, prot.cfg.MaxPeers),
		quit:                   make(chan struct{}),
		modifyRebroadcastInv:   make(chan interface{}),
		peerHeightsUpdate:      make(chan updatePeerHeightsMsg, 1000),
		peerMinerHeightsUpdate: make(chan updatePeerHeightsMsg, 1000),
		nat:                    nat,
		db:                     db,
		minerdb:                minerdb,
		timeSource:             chainutil.NewMedianTime(),
		services:               services,
		//		sigCache:             NewSigCache(cfg.SigCacheMaxSize),
		//		hashCache:            NewHashCache(cfg.SigCacheMaxSize),
		cfCheckptCaches: make(map[wire.FilterType][]cfHeaderKV),
		signAddress:     prot.cfg.signAddress,
		privKeys:        prot.cfg.privateKeys,
		//		BlackList:            make(map[[20]byte]struct{}),
		//		PendingBlackList:     make(map[[20]byte]uint32),
		Broadcasted: make(map[chainhash.Hash]int64),
	}
	s.prot = prot
	s.chainParams.Net = prot.cfg.NetMagic

	if prot.cfg.Generate && !prot.cfg.TxIndex { // must allow txindex when mining
		return nil, errors.New("Must enable tx index (width full history) when mining.")
	}

	// Create the transaction and address indexes if needed.
	//
	// CAUTION: the txindex needs to be first in the indexes array because
	// the addrindex uses data from the txindex during catchup.  If the
	// addrindex is run first, it may not have the transactions from the
	// current block indexed.
	var indexes []indexers.Indexer

	prot.cfg.TxIndex, prot.cfg.AddrIndex = true, true // it's now mandatory
	if prot.IsSvp {
		prot.cfg.TxIndex, prot.cfg.AddrIndex = false, false // it's now mandatory
	}

	if prot.cfg.TxIndex || prot.cfg.AddrIndex {
		// Enable transaction index if address index is enabled since it
		// requires it.
		if !prot.cfg.TxIndex {
			indxLog.Infof("Transaction index enabled because it " +
				"is required by the address index")
			prot.cfg.TxIndex = true
		} else {
			indxLog.Info("Transaction index is enabled")
		}

		s.txIndex = indexers.NewTxIndex(db)
		indexes = append(indexes, s.txIndex)
	}
	if prot.cfg.AddrIndex {
		indxLog.Info("Address index is enabled")
		s.addrIndex = indexers.NewAddrIndex(db, prot.activeNetParams)
		indexes = append(indexes, s.addrIndex)
	}

	if !prot.IsSvp {
		s.addrUseIndex = indexers.NewAddrUseIndex(db, prot.activeNetParams)
		indexes = append(indexes, s.addrUseIndex)
	}

	// Create an index manager if any of the optional indexes are enabled.
	var indexManager blockchain.IndexManager
	if len(indexes) > 0 && !prot.IsSvp {
		indexManager = indexers.NewManager(db, indexes)
	}

	// Merge given checkpoints with the default ones unless they are disabled.
	var checkpoints []chaincfg.Checkpoint
	if !prot.cfg.DisableCheckpoints {
		checkpoints = mergeCheckpoints(s.chainParams.Checkpoints, prot.cfg.addCheckpoints)
	}

	// Create a new block chain instance with the appropriate configuration.
	var err error
	if !prot.cfg.Passive {
		s.chain, err = minerchain.New(&blockchain.Config{
			DB:          s.db,
			MinerDB:     s.minerdb,
			Interrupt:   interrupt,
			ChainParams: s.chainParams,
			Checkpoints: checkpoints,
			TimeSource:  s.timeSource,
			//		SigCache:     s.sigCache,
			IndexManager: indexManager,
			Miner:        prot.cfg.signAddress,
			PrivKey:      prot.cfg.privateKeys,
			AddrUsage:    s.addrUseIndex.Usage,
			IsSVP:        prot.IsSvp,
			//		HashCache:    s.hashCache,
		}, shutdownRequestChannel)
		if err != nil {
			return nil, err
		}

		s.chain.Subscribe(s.chain.TphNotice)
		// go s.handleSrvReq()

		// Search for a FeeEstimator state in the database. If none can be found
		// or if it cannot be loaded, create a new one.

		db.Update(func(tx database.Tx) error {
			metadata := tx.Metadata()
			feeEstimationData := metadata.Get(mempool.EstimateFeeDatabaseKey)
			if feeEstimationData != nil {
				// delete it from the database so that we don't try to restore the
				// same thing again somehow.
				metadata.Delete(mempool.EstimateFeeDatabaseKey)

				// If there is an error, log it and make a new fee estimator.
				var err error
				s.feeEstimator, err = mempool.RestoreFeeEstimator(feeEstimationData)

				if err != nil {
					peerLog.Errorf("Failed to restore fee estimator %v", err)
				}
			}

			return nil
		})

		s.chain.InitCollateral()

		// If no feeEstimator has been found, or if the one that has been found
		// is behind somehow, create a new one and start over.
		if s.feeEstimator == nil || s.feeEstimator.LastKnownHeight() != s.chain.BestSnapshot().Height {
			s.feeEstimator = mempool.NewFeeEstimator(
				mempool.DefaultEstimateFeeMaxRollback,
				mempool.DefaultEstimateFeeMinRegisteredBlocks)
		}

		txC := mempool.Config{
			Policy: mempool.Policy{
				Mining:               prot.cfg.Generate || !prot.cfg.DisablePOWMining,
				DisableRelayPriority: prot.cfg.NoRelayPriority,
				AcceptNonStd:         prot.cfg.RelayNonStd,
				FreeTxRelayLimit:     prot.cfg.FreeTxRelayLimit,
				MaxOrphanTxs:         prot.cfg.MaxOrphanTxs,
				MaxOrphanTxSize:      defaultMaxOrphanTxSize,
				MaxSigOpCostPerTx:    chaincfg.MaxBlockSigOpsCost / 4,
				MinRelayTxFee:        prot.cfg.minRelayTxFee,
				MaxTxVersion:         2,
			},
			ChainParams:   prot.activeNetParams,
			FetchUtxoView: s.chain.FetchUtxoView,
			//		Views: s.chain.NewViewPointSet(),
			BestHeight:     func() int32 { return s.chain.BestSnapshot().Height },
			MedianTimePast: func() time.Time { return s.chain.BestSnapshot().MedianTime },
			CalcSequenceLock: func(tx *btcutil.Tx, view *viewpoint.UtxoViewpoint) (*blockchain.SequenceLock, error) {
				return s.chain.CalcSequenceLock(tx, view, true)
			},
			IsDeploymentActive: s.chain.Miners.IsDeploymentActive,
			//		SigCache:           s.sigCache,
			//		HashCache:          s.hashCache,
			AddrIndex:    s.addrIndex,
			FeeEstimator: s.feeEstimator,
		}
		s.txMemPool = mempool.New(&txC)
		//	s.txMemPool.Blacklist = &s

		s.syncManager, err = netsync.New(&netsync.Config{
			PeerNotifier:       &s,
			Chain:              s.chain,
			TxMemPool:          s.txMemPool,
			ChainParams:        s.chainParams,
			DisableCheckpoints: prot.cfg.DisableCheckpoints,
			MaxPeers:           prot.cfg.MaxPeers,
			FeeEstimator:       s.feeEstimator,
			Passive:            prot.cfg.Passive,
		})
		if err != nil {
			return nil, err
		}

		//	s.chain.Blacklist = &s

		// Create the mining policy and block template generator based on the
		// configuration options.
		//
		// NOTE: The CPU miner relies on the mempool, so the mempool has to be
		// created before calling the function to create the CPU miner.
		policy := mining.Policy{
			BlockPrioritySize: prot.cfg.BlockPrioritySize,
			MinBlockWeight:    prot.cfg.MinBlockWeight,
			TxMinFreeFee:      prot.cfg.minRelayTxFee,
		}
		blockTemplateGenerator := mining.NewBlkTmplGenerator(&policy,
			s.chainParams, s.txMemPool, s.chain, s.timeSource, prot.activeNetParams)
		//		s.sigCache, s.hashCache)
		// This is the miner for Tx chain

		prot.db.View(func(tx database.Tx) error {
			bucket := tx.Metadata().Bucket([]byte("blacklist"))
			for _, s := range prot.cfg.Blacklist {
				addr, _ := btcutil.DecodeAddress(s, prot.activeNetParams)
				bucket.Put(addr.ScriptNetAddress()[:21], []byte(s))
			}
			return nil
		})

		s.minerMiner = nil
		s.cpuMiner = nil
		if prot.IsSvp {
			prot.cfg.GenerateMiner = false
			prot.cfg.Generate = false
		} else {
			mcfg := &minerchain.Config{
				ChainParams:            prot.activeNetParams,
				MinerWorkers:           prot.cfg.MinerWorkers,
				BlockTemplateGenerator: blockTemplateGenerator,
				ProcessBlock:           s.syncManager.ProcessMinerBlock,
				ConnectedCount:         s.ConnectedCount,
				IsCurrent:              s.syncManager.IsCurrent,
				ExternalIPs:            prot.cfg.ExternalIPs,
			}

			for _, pk := range s.chain.PrivKey {
				mcfg.Privkeys = append(mcfg.Privkeys, pk)
				wif, _ := btcutil.NewWIF(pk, prot.activeNetParams, true)

				s.chain.Miners.AddMiningKey(wif.String())
			}

			s.cpuMiner = cpuminer.New(&cpuminer.Config{
				ChainParams:            prot.activeNetParams,
				BlockTemplateGenerator: blockTemplateGenerator,
				MiningAddrs:            prot.cfg.miningAddrs,
				SignAddress:            prot.cfg.signAddress,
				PrivKeys:               s.chain.PrivKey,
				DisablePOWMining:       prot.cfg.DisablePOWMining,

				ProcessBlock:   s.syncManager.ProcessBlock,
				ConnectedCount: s.ConnectedCount,
				IsCurrent:      s.syncManager.IsCurrent,
				Generate:       prot.cfg.Generate,
			})

			mcfg.TxGenerate = prot.cfg.Generate
			if prot.cfg.GenerateMiner {
				if len(prot.cfg.signAddress) > 0 {
					mcfg.MiningAddrs = prot.cfg.signAddress
				} else {
					mcfg.MiningAddrs = prot.cfg.miningAddrs
				}
				s.minerMiner = minerchain.NewMiner(mcfg)
			} else {
				s.minerMiner = nil
				prot.cfg.GenerateMiner = false
			}
		}

		if !prot.cfg.DisableRPC {
			// Setup listeners for the configured RPC listen addresses and
			// TLS settings.
			rpcListeners, err := setupRPCListeners(prot.cfg)
			if err != nil {
				return nil, err
			}
			if len(rpcListeners) == 0 {
				return nil, errors.New("RPCS: No valid listen address")
			}

			s.rpcServer, err = newRPCServer(&rpcserverConfig{
				Cfg:          prot.cfg,
				Listeners:    rpcListeners,
				StartupTime:  s.startupTime,
				ConnMgr:      &rpcConnManager{&s},
				SyncMgr:      &rpcSyncMgr{&s, s.syncManager},
				TimeSource:   s.timeSource,
				Chain:        s.chain,
				ChainParams:  prot.activeNetParams,
				DB:           db,
				MinerDB:      minerdb,
				TxMemPool:    s.txMemPool,
				Generator:    blockTemplateGenerator,
				CPUMiner:     s.cpuMiner,
				MinerMiner:   s.minerMiner,
				TxIndex:      s.txIndex,
				AddrIndex:    s.addrIndex,
				FeeEstimator: s.feeEstimator,
			})

			if err != nil {
				return nil, err
			}

			// Signal process shutdown when the RPC server requests it.
			go func() {
				btcdLog.Infof("shutdownRequestChannel <- s.rpcServer.RequestedProcessShutdown")
				<-s.rpcServer.RequestedProcessShutdown()
				shutdownRequestChannel <- struct{}{}
			}()
		}

		s.chain.Subscribe(func(notification *blockchain.Notification) {
			if notification.Type != blockchain.NTBlockConnected {
				return
			}
			block := notification.Data.(*btcutil.Block)
			if block == nil {
				return
			}
			for _, tx := range block.Transactions()[1:] {
				s.txMemPool.RemoveTransaction(tx, false)
			}
		})
	} else {
		s.syncManager, err = netsync.New(&netsync.Config{
			PeerNotifier:       &s,
			Chain:              s.chain,
			TxMemPool:          s.txMemPool,
			ChainParams:        s.chainParams,
			DisableCheckpoints: prot.cfg.DisableCheckpoints,
			MaxPeers:           prot.cfg.MaxPeers,
			FeeEstimator:       s.feeEstimator,
			Passive:            prot.cfg.Passive,
		})
		if err != nil {
			return nil, err
		}
	}

	// Only setup a function to return new addresses to connect to when
	// not running in connect-only mode.  The simulation network is always
	// in connect-only mode since it is only intended to connect to
	// specified peers and actively avoid advertising and connecting to
	// discovered peers in order to prevent it from becoming a public test
	// network.
	var newAddressFunc func() (net.Addr, error)
	if !prot.cfg.SimNet && len(prot.cfg.ConnectPeers) == 0 {
		newAddressFunc = func() (net.Addr, error) {
			for tries := 0; tries < 100; tries++ {
				addr := s.addrManager.GetAddress()
				if addr == nil {
					break
				}

				// Address will not be invalid, local or unroutable
				// because addrmanager rejects those on addition.
				// Just check that we don't already have an address
				// in the same group so that we are not connecting
				// to the same network segment at the expense of
				// others.
				key := addrmgr.GroupKey(addr.NetAddress())
				if s.OutboundGroupCount(key) != 0 {
					continue
				}

				// only allow recent nodes (10mins) after we failed 30
				// times
				if tries < 30 && time.Since(addr.LastAttempt()) < 10*time.Minute {
					continue
				}

				// allow nondefault ports after 50 failed tries.
				if tries < 50 && fmt.Sprintf("%d", addr.NetAddress().Port) != prot.activeNetParams.DefaultPort {
					continue
				}

				addrString := addrmgr.NetAddressKey(addr.NetAddress())
				return addrStringToNetAddr(addrString, prot.cfg)
			}

			return nil, errors.New("no valid connect address")
		}
	}

	// Create a connection manager.
	targetOutbound := defaultTargetOutbound
	if prot.cfg.MaxPeers < targetOutbound {
		targetOutbound = prot.cfg.MaxPeers
	}

	cfgdbtcdDial := func(addr net.Addr) (net.Conn, error) {
		return btcdDial(addr, prot.cfg)
	}

	cmgr, err := connmgr.New(&connmgr.Config{
		Listeners:      listeners,
		OnAccept:       s.inboundPeerConnected,
		RetryDuration:  connectionRetryInterval,
		TargetOutbound: uint32(targetOutbound),
		Dial:           cfgdbtcdDial,
		OnConnection:   s.outboundPeerConnected,
		GetNewAddress:  newAddressFunc,
	})
	if err != nil {
		return nil, err
	}
	s.connManager = cmgr

	// Start up persistent peers.
	permanentPeers := prot.cfg.ConnectPeers
	if len(permanentPeers) == 0 {
		permanentPeers = prot.cfg.AddPeers
	}
	for _, addr := range permanentPeers {
		netAddr, err := addrStringToNetAddr(addr, prot.cfg)
		if err != nil {
			return nil, err
		}

		go s.connManager.Connect(&connmgr.ConnReq{
			Addr:      netAddr,
			Permanent: true,
		})
	}

	return &s, nil
}

// initListeners initializes the configured net listeners and adds any bound
// addresses to the address manager. Returns the listeners and a NAT interface,
// which is non-nil if UPnP is in use.
func initListeners(amgr *addrmgr.AddrManager, listenAddrs []string, services common.ServiceFlag, cfg *Protocol) ([]net.Listener, NAT, error) {
	// Listen for TCP connections at the configured addresses
	netAddrs, err := parseListeners(listenAddrs)
	if err != nil {
		return nil, nil, err
	}

	listeners := make([]net.Listener, 0, len(netAddrs))
	for _, addr := range netAddrs {
		listener, err := net.Listen(addr.Network(), addr.String())
		if err != nil {
			srvrLog.Warnf("Can't listen on %s: %v", addr, err)
			continue
		}
		listeners = append(listeners, listener)
	}

	var nat NAT
	if len(cfg.cfg.ExternalIPs) != 0 {
		defaultPort, err := strconv.ParseUint(cfg.activeNetParams.DefaultPort, 10, 16)
		if err != nil {
			srvrLog.Errorf("Can not parse default port %s for active chain: %v",
				cfg.activeNetParams.DefaultPort, err)
			return nil, nil, err
		}

		for _, sip := range cfg.cfg.ExternalIPs {
			eport := uint16(defaultPort)
			host, portstr, err := net.SplitHostPort(sip)
			if err != nil {
				// no port, use default.
				host = sip
			} else {
				port, err := strconv.ParseUint(portstr, 10, 16)
				if err != nil {
					srvrLog.Warnf("Can not parse port from %s for "+
						"externalip: %v", sip, err)
					continue
				}
				eport = uint16(port)
			}
			na, err := amgr.HostToNetAddress(host, eport, services)
			if err != nil {
				srvrLog.Warnf("Not adding %s as externalip: %v", sip, err)
				continue
			}

			_, err = amgr.AddLocalAddress(na, addrmgr.ManualPrio)
			if err != nil {
				amgrLog.Warnf("Skipping specified external IP: %v", err)
			}
		}
	} else {
		if cfg.cfg.Upnp {
			var err error
			nat, err = Discover()
			if err != nil {
				srvrLog.Warnf("Can't discover upnp: %v", err)
			}
			// nil nat here is fine, just means no upnp on network.
		}

		// Add bound addresses to address manager to be advertised to peers.
		for _, listener := range listeners {
			addr := listener.Addr().String()
			err := addLocalAddress(amgr, addr, services)
			if err != nil {
				amgrLog.Warnf("Skipping bound address %s: %v", addr, err)
			}
		}
	}

	return listeners, nat, nil
}

// addrStringToNetAddr takes an address in the form of 'host:port' and returns
// a net.Addr which maps to the original address with any host names resolved
// to IP addresses.  It also handles tor addresses properly by returning a
// net.Addr that encapsulates the address.
func addrStringToNetAddr(addr string, cfg *config) (net.Addr, error) {
	host, strPort, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(strPort)
	if err != nil {
		return nil, err
	}

	// Skip if host is already an IP address.
	if ip := net.ParseIP(host); ip != nil {
		return &net.TCPAddr{
			IP:   ip,
			Port: port,
		}, nil
	}

	// Tor addresses cannot be resolved to an IP, so just return an onion
	// address instead.
	if strings.HasSuffix(host, ".onion") {
		if cfg.NoOnion {
			return nil, errors.New("tor has been disabled")
		}

		return &onionAddr{addr: addr}, nil
	}

	// Attempt to look up an IP address associated with the parsed host.
	ips, err := btcdLookup(host, cfg)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}

	return &net.TCPAddr{
		IP:   ips[0],
		Port: port,
	}, nil
}

// addLocalAddress adds an address that this node is listening on to the
// address manager so that it may be relayed to peers.
func addLocalAddress(addrMgr *addrmgr.AddrManager, addr string, services common.ServiceFlag) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return err
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		// If bound to unspecified address, advertise all local interfaces
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return err
		}

		for _, addr := range addrs {
			ifaceIP, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}

			// If bound to 0.0.0.0, do not add IPv6 interfaces and if bound to
			// ::, do not add IPv4 interfaces.
			if (ip.To4() == nil) != (ifaceIP.To4() == nil) {
				continue
			}

			netAddr := wire.NewNetAddressIPPort(ifaceIP, uint16(port), services)
			addrMgr.AddLocalAddress(netAddr, addrmgr.BoundPrio)
		}
	} else {
		netAddr, err := addrMgr.HostToNetAddress(host, uint16(port), services)
		if err != nil {
			return err
		}

		addrMgr.AddLocalAddress(netAddr, addrmgr.BoundPrio)
	}

	return nil
}

// dynamicTickDuration is a convenience function used to dynamically choose a
// tick duration based on remaining time.  It is primarily used during
// server shutdown to make shutdown warnings more frequent as the shutdown time
// approaches.
func dynamicTickDuration(remaining time.Duration) time.Duration {
	switch {
	case remaining <= time.Second*5:
		return time.Second
	case remaining <= time.Second*15:
		return time.Second * 5
	case remaining <= time.Minute:
		return time.Second * 15
	case remaining <= time.Minute*5:
		return time.Minute
	case remaining <= time.Minute*15:
		return time.Minute * 5
	case remaining <= time.Hour:
		return time.Minute * 15
	}
	return time.Hour
}

// isWhitelisted returns whether the IP address is included in the whitelisted
// networks and IPs.
func isWhitelisted(addr net.Addr, cfg *config) bool {
	if len(cfg.whitelists) == 0 {
		return false
	}

	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		srvrLog.Warnf("Unable to SplitHostPort on '%s': %v", addr, err)
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		srvrLog.Warnf("Unable to parse IP '%s'", addr)
		return false
	}

	for _, ipnet := range cfg.whitelists {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// checkpointSorter implements sort.Interface to allow a slice of checkpoints to
// be sorted.
type checkpointSorter []chaincfg.Checkpoint

// Len returns the number of checkpoints in the slice.  It is part of the
// sort.Interface implementation.
func (s checkpointSorter) Len() int {
	return len(s)
}

// Swap swaps the checkpoints at the passed indices.  It is part of the
// sort.Interface implementation.
func (s checkpointSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less returns whether the checkpoint with index i should sort before the
// checkpoint with index j.  It is part of the sort.Interface implementation.
func (s checkpointSorter) Less(i, j int) bool {
	return s[i].Height < s[j].Height
}

// mergeCheckpoints returns two slices of checkpoints merged into one slice
// such that the checkpoints are sorted by height.  In the case the additional
// checkpoints contain a checkpoint with the same height as a checkpoint in the
// default checkpoints, the additional checkpoint will take precedence and
// overwrite the default one.
func mergeCheckpoints(defaultCheckpoints, additional []chaincfg.Checkpoint) []chaincfg.Checkpoint {
	// Create a map of the additional checkpoints to remove duplicates while
	// leaving the most recently-specified checkpoint.
	extra := make(map[int32]chaincfg.Checkpoint)
	for _, checkpoint := range additional {
		extra[checkpoint.Height] = checkpoint
	}

	// Add all default checkpoints that do not have an override in the
	// additional checkpoints.
	numDefault := len(defaultCheckpoints)
	checkpoints := make([]chaincfg.Checkpoint, 0, numDefault+len(extra))
	for _, checkpoint := range defaultCheckpoints {
		if _, exists := extra[checkpoint.Height]; !exists {
			checkpoints = append(checkpoints, checkpoint)
		}
	}

	// Append the additional checkpoints and return the sorted results.
	for _, checkpoint := range extra {
		checkpoints = append(checkpoints, checkpoint)
	}
	sort.Sort(checkpointSorter(checkpoints))
	return checkpoints
}
