// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2018-2021 The Omegasuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chaincfg

import (
	"errors"
	"math/big"
	"time"

	"btcd/wire"
	"btcd/wire/common"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
)

// These variables are the chain proof-of-work limit parameters for each default
// network.
var (
	// bigOne is 1 represented as a big.Int.  It is defined here to avoid
	// the overhead of creating it multiple times.
	bigOne = big.NewInt(1)

	// mainPowLimit is the highest proof of work value a Bitcoin block can
	// have for the main network.  It is the value 2^240 - 1.
	mainPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 240), bigOne)

	// regressionPowLimit is the highest proof of work value a Bitcoin block
	// can have for the regression test network.  It is the value 2^255 - 1.
	regressionPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

	// testNet3PowLimit is the highest proof of work value a Bitcoin block
	// can have for the test network (version 3).  It is the value
	// 2^240 - 1.
	testNet3PowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 250), bigOne)

	// simNetPowLimit is the highest proof of work value a Bitcoin block
	// can have for the simulation test network.  It is the value 2^255 - 1.
	simNetPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)
)

const (
	// MaxBlockSigOpsCost is the maximum number of signature operations
	// allowed for a block.
	MaxBlockSigOpsCost = 80000

	// MaxOutputsPerBlock is the maximum number of transaction outputs there
	// can be in a block of max weight size.
	MaxOutputsPerTx = 1000
)

// Checkpoint identifies a known good point in the block chain.  Using
// checkpoints allows a few optimizations for old blocks during initial download
// and also prevents forks from old blocks.
//
// Each checkpoint is selected based upon several factors.  See the
// documentation for blockchain.IsCheckpointCandidate for details on the
// selection criteria.
type Checkpoint struct {
	Height int32
	Hash   *chainhash.Hash
}

// DNSSeed identifies a DNS seed.
type DNSSeed struct {
	// Host defines the hostname of the seed.
	Host string

	// HasFiltering defines whether the seed supports filtering
	// by service flags (wire.ServiceFlag).
	HasFiltering bool
}

// ConsensusDeployment defines details related to a specific consensus rule
// change that is voted in.  This is part of BIP0009.
type ConsensusDeployment struct {
	// PrevVersion defines the previous version that this deployment is based on
	PrevVersion uint32

	// FeatureMask defines the specific features within the block version
	// this particular soft-fork deployment refers to.
	FeatureMask uint32

	// StartTime is the median block time after which voting on the
	// deployment starts.
	StartTime uint64

	// ExpireTime is the median block time after which the attempted
	// deployment expires.
	ExpireTime uint64
}

// Constants that define the deployment offset in the deployments field of the
// parameters for each deployment.  This is useful to be able to get the details
// of a specific deployment by name.
const (
	// DeploymentTestDummy defines the rule change deployment ID for testing
	// purposes.
	DeploymentTestDummy = iota

	// DefinedDeployments is the number of currently defined deployments.
	// It must always come last since it is used to determine how many
	// defined deployments there currently are.
	DefinedDeployments
)

const DefaultChainID = 1       // ID of this chain. each chain has a unique id
const DefaultParentChainID = 0 // ID of this chain. each chain has a unique id

const (
	Version1 = 0x10000

	SVPVersion1 = 0x10000
)

type forfeitureContract struct {
	Contract [21]byte
	Opening  [4]byte
	Filing   [4]byte
	Claim    [4]byte
}

type GlobalParams struct { // The params that must be the same for every node in the blockchain
	// Name defines a human-readable identifier for the network.
	Name string

	// Net defines the magic bytes used to identify the network.
	Net uint32

	// DefaultPort defines the default peer-to-peer port for the network.
	DefaultPort string
	RpcPort     string // default rpc port

	// DNSSeeds defines a list of DNS seeds for the network that are used
	// as one method to discover peers.
	DNSSeeds []DNSSeed

	// PowLimitBits defines the highest allowed proof of work value for a
	// block in compact form.
	PowLimitBits uint32

	// CoinbaseMaturity is the number of blocks required before newly mined
	// coins (coinbase transactions) can be spent.
	CoinbaseMaturity uint16

	// SubsidyReductionInterval is the interval of blocks before the subsidy
	// is reduced.
	SubsidyReductionInterval int32

	MinimalAward int64

	// TargetTimespan is the desired amount of time that should elapse
	// before the block difficulty requirement is examined to determine how
	// it should be changed in order to maintain the desired block
	// generation rate.
	TargetTimespan time.Duration

	// TargetTimePerBlock is the desired amount of time to generate each
	// block.
	TargetTimePerBlock time.Duration

	// RetargetAdjustmentFactor is the adjustment factor used to limit
	// the minimum and maximum amount of adjustment that can occur between
	// difficulty retargets.
	RetargetAdjustmentFactor int64

	// ReduceMinDifficulty defines whether the network should reduce the
	// minimum required difficulty after a long enough period of time has
	// passed without finding a block.  This is really only useful for test
	// networks and should not be set on a main network.
	//	ReduceMinDifficulty bool

	// These fields are related to voting on consensus rule changes as
	// defined by BIP0009.
	//
	// RuleChangeActivationThreshold is the number of blocks in a threshold
	// state retarget window for which a positive vote for a rule change
	// must be cast in order to lock in a rule change. It should typically
	// be 95% for the main network and 75% for test networks.
	//
	// MinerConfirmationWindow is the number of blocks in each threshold
	// state retarget window.
	//
	// Deployments define the specific consensus rule changes to be voted
	// on.
	RuleChangeActivationThreshold uint32
	MinerConfirmationWindow       uint32

	// H1DenominatorActivationStartHeight enables the miner-chain h1
	// denominator rule that uses the parent miner block's Collateral.  A
	// value of zero keeps the legacy current-header denominator.
	H1DenominatorActivationStartHeight int32

	// H1DenominatorActivationStopHeight returns the h1 denominator rule to
	// the legacy current-header Collateral at and after this height.  A
	// value of zero means the parent-denominator interval does not stop.
	// Together with the start height, this is a non-retroactive schedule:
	// once a height is mined, changing the interval that covers it is a
	// consensus rule change.
	H1DenominatorActivationStopHeight int32

	// forfeiture
	Forfeit                 forfeitureContract
	ViolationReportDeadline int32

	ChainID uint32

	PowLimit *big.Int
	// Checkpoints ordered from oldest to newest.
	Checkpoints          []Checkpoint
	Deployments          []ConsensusDeployment
	MinBorderFee         int
	MinContractDeployFee int // Min fee per byte for contract deployment
	MinRelayTxFee        int64
	ContractExecFee      int64 // contract execution cost as Haos per 10K steps
}

// Params defines a Bitcoin network by its parameters.  These parameters may be
// used by Bitcoin applications to differentiate networks as well as addresses
// and keys for one network from those intended for use on another network.
type Params struct {
	GlobalParams

	// GenesisHash is the starting block hash.
	GenesisHash      *chainhash.Hash
	GenesisMinerHash *chainhash.Hash

	// PowLimit defines the highest allowed proof of work value for a block
	// as a uint256.
	PowLimit *big.Int

	// parent chainid of main chain
	ParentChainId uint32

	// external IPs that peers can reach us
	ExternalIPs []string

	// GenesisBlock defines the first block of the chain.
	GenesisBlock      *wire.MsgBlock
	GenesisMinerBlock *wire.MingingRightBlock

	// ChainCurrentStd is the latest best block time for chain to be
	// considered corrent. Default is 24 hours
	ChainCurrentStd time.Duration

	// GenerateSupported specifies whether or not CPU mining is allowed.
	GenerateSupported bool

	// Checkpoints ordered from oldest to newest.
	Checkpoints []Checkpoint

	Deployments []ConsensusDeployment

	// Mempool parameters
	// RelayNonStdTxs bool

	// Human-readable part for Bech32 encoded segwit addresses, as defined
	// in BIP 173.
	// Bech32HRPSegwit string

	// Address encoding magics
	PubKeyHashAddrID byte // First byte of a P2PKH address
	MultiSigAddrID   byte // First byte of a multisig address
	MultiSigAddrXID  byte // First byte of a multisig redeem script
	ScriptHashAddrID byte // First byte of a P2SH address
	ScriptAddrID     byte // First byte of a P2SH address
	ContractAddrID   byte // First byte of a P2C address
	PrivateKeyID     byte // First byte of a WIF private key

	// BIP32 hierarchical deterministic extended key magics
	// HDPublicKeyID  [4]byte
	// HDPrivateKeyID [4]byte

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	// HDCoinType uint32

	// ContractExecLimit is a policy by each node to limit step a contract may execute
	ContractExecLimit uint32

	// SigVeriConcurrency is the number of concurrent verifiers for signature veridfication
	SigVeriConcurrency int

	MinBorderFee         int
	MinContractDeployFee int // Min fee per byte for contract deployment
	MinRelayTxFee        int64
	ContractExecFee      int64 // contract execution cost as Haos per 10K steps

	// local rule: require expiration time set if tx has contract
	ContractReqExp bool

	// whether we log time blocks received
	LogBlockTime bool

	MainChainID uint32

	AddChain interface{}
	AddDns   interface{}
}

// MainNetParams defines the network parameters for the main Omega network.
var MainNetParams = Params{
	GlobalParams: GlobalParams{
		Name:        "mainnet",
		Net:         uint32(common.MainNet),
		DefaultPort: "9788",
		DNSSeeds: []DNSSeed{
			{"omegasuite.org", false},
			{"78.141.214.76", false},
			{"207.246.106.17", false},
			{"78.141.236.245", false},
		},
		PowLimitBits:             0x1e000ff0,
		CoinbaseMaturity:         1000 * wire.MINER_RORATE_FREQ,
		SubsidyReductionInterval: 2102400 * wire.MINER_RORATE_FREQ,
		MinimalAward:             0,
		TargetTimespan:           time.Hour * 24 * 14, // adjust every 14 days
		TargetTimePerBlock:       time.Minute * 1,     // 1 minutes/block
		RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
		// Consensus rule change deployments.
		//
		// The miner confirmation window is defined as:
		//   target proof of work timespan / target proof of work spacing
		RuleChangeActivationThreshold: 19160, // 95% of MinerConfirmationWindow
		MinerConfirmationWindow:       20160, //
		Forfeit: forfeitureContract{
			Contract: [21]byte{0x88, 0x1a, 0x52, 0x0f, 0xa9, 0x4d, 0x8e, 0x07,
				0x3b, 0x0b, 0x46, 0x79, 0x43, 0x5b, 0x55, 0x09, 0xa5, 0xc6, 0x84, 0x7d, 0xb3},
			Opening: [4]byte{0x7c, 0xef, 0x8a, 0x73},
			Filing:  [4]byte{0xb2, 0x18, 0x16, 0x5a},
			Claim:   [4]byte{0x44, 0x90, 0x02, 0xf8},
		},
		ViolationReportDeadline: wire.DESIRABLE_MINER_CANDIDATES + 100,
		ChainID:                 DefaultChainID, // Omega
		RpcPort:                 "9789",
	},

	GenesisHash:      GenesisHash[uint32(common.MainNet)],
	GenesisMinerHash: GenesisMinerHash[uint32(common.MainNet)],
	PowLimit:         mainPowLimit,

	ParentChainId: DefaultParentChainID,

	// Chain parameters
	GenesisBlock:         GenesisBlock[uint32(common.MainNet)],
	GenesisMinerBlock:    GenesisMinerBlock[uint32(common.MainNet)],
	ChainCurrentStd:      time.Hour * 24,
	MinBorderFee:         1000,
	MinContractDeployFee: 1000,
	//	ReduceMinDifficulty:      false,
	GenerateSupported: false,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: []Checkpoint{},

	Deployments: []ConsensusDeployment{
		DeploymentTestDummy: {
			PrevVersion: 0,
			FeatureMask: 0,
			StartTime:   1199145601, // January 1, 2008 UTC
			ExpireTime:  1757289600, // Sept 8, 2025 UTC
		},
	},

	// Mempool parameters
	// RelayNonStdTxs: false,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	// Bech32HRPSegwit: "bc", // always bc for main net

	// Address encoding magics
	PubKeyHashAddrID: 0x00, // starts with 1
	MultiSigAddrID:   0x78,
	MultiSigAddrXID:  0xC3,

	ScriptHashAddrID: 0x05, // starts with 3
	ScriptAddrID:     0x13,
	ContractAddrID:   0x88, // start with 8
	PrivateKeyID:     0x80, // starts with 5 (uncompressed) or K (compressed)

	// HDPublicKeyID:  [4]byte{0x04, 0x88, 0xad, 0xe4},
	// HDPrivateKeyID: [4]byte{0x04, 0x88, 0xb2, 0x1e},

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	// HDCoinType:        0,
	ContractExecLimit: 10000, // min limit of total contract execution steps in a block
	ContractExecFee:   1,
	ContractReqExp:    false,
}

// RegressionNetParams defines the network parameters for the regression test
// Bitcoin network.  Not to be confused with the test Bitcoin network (version
// 3), this network is sometimes simply called "testnet".
/*
var RegressionNetParams = Params{
	GlobalParams: GlobalParams{
		Name:                     "regtest",
		Net:                      common.RegNet,
		DefaultPort:              "18484",
		DNSSeeds:                 []DNSSeed{},
		PowLimitBits:             0x207fffff,
		CoinbaseMaturity:         10,
		SubsidyReductionInterval: 150 * wire.MINER_RORATE_FREQ,
		MinimalAward:             0,
		TargetTimespan:           time.Hour * 24 * 14, // 14 days
		TargetTimePerBlock:       time.Minute * 1,    // 10 minutes
		RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
		MinDiffReductionTime:     time.Minute * 20,    // TargetTimePerBlock * 2
		// Consensus rule change deployments.
		//
		// The miner confirmation window is defined as:
		//   target proof of work timespan / target proof of work spacing
		RuleChangeActivationThreshold: 75, // 75%  of MinerConfirmationWindow
		MinerConfirmationWindow:       100,
		Forfeit: forfeitureContract{
			Contract: [21]byte{0x88, 0xeb, 0xa5, 0x7d, 0xba, 0x8e, 0x88, 0x3e, 0x96, 0x2b,
				0x1f, 0x13, 0xe7, 0xb0, 0xf3, 0x7f, 0x6d, 0x3b, 0x48, 0x48, 0xfc},
			Opening: [4]byte{0x7c, 0xef, 0x8a, 0x73},
			Filing:  [4]byte{0xb2, 0x18, 0x16, 0x5a},
			Claim:   [4]byte{0x44, 0x90, 0x02, 0xf8},
		},
		ViolationReportDeadline: 10,
		ChainID:                 DefaultChainID, // Omega
		RpcPort:                 "18840",
	},

	GenesisHash:      &regTestGenesisHash,
	GenesisMinerHash: &regTestGenesisMinerHash,
	PowLimit:         regressionPowLimit,

	ParentChainId: DefaultParentChainID,
	// Chain parameters
	GenesisBlock:      &regTestGenesisBlock,
	GenesisMinerBlock: &regTestGenesisMinerBlock,
	ChainCurrentStd:   time.Hour * 24000,
	MinBorderFee:      100000,
	//	ReduceMinDifficulty:      true,
	GenerateSupported: true,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: nil,

	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			PrevVersion: 0,
			FeatureMask: 0,
			StartTime:   0,             // Always available for vote
			ExpireTime:  math.MaxInt64, // Never expires
		},
	},

	// Mempool parameters
	RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "bcrt", // always bcrt for reg test net

	// Address encoding magics
	PubKeyHashAddrID: 0x6f, // starts with m or n
	MultiSigAddrID:   0x67,
	MultiSigAddrXID:  0xC3,

	ScriptHashAddrID: 0xc4, // starts with 2
	ScriptAddrID:     0x13,
	ContractAddrID:   0x88, // start with 8
	PrivateKeyID:     0xef, // starts with 9 (uncompressed) or c (compressed)

	HDPublicKeyID:  [4]byte{0x04, 0x35, 0x83, 0x94},
	HDPrivateKeyID: [4]byte{0x04, 0x35, 0x87, 0xcf},

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 1,

	ContractExecLimit: 10000,
	ContractExecFee:   1,
	ContractReqExp:    false,
}
*/

// TestNet3Params defines the network parameters for the test Bitcoin network
// (version 3).  Not to be confused with the regression test network, this
// network is sometimes simply called "testnet".
var TestNet3Params = Params{
	GlobalParams: GlobalParams{
		Name:        "testnet",
		Net:         uint32(common.TestNet),
		DefaultPort: "7788",
		DNSSeeds: []DNSSeed{
			{"omegasuite.org", false},
		},
		PowLimitBits:             0x1f0fffff, // 0x1d3fffff
		CoinbaseMaturity:         10,
		SubsidyReductionInterval: 2102400 * wire.MINER_RORATE_FREQ,
		MinimalAward:             0,
		TargetTimespan:           time.Hour * 2, // 2 hours
		TargetTimePerBlock:       time.Minute,   // 1 minutes
		RetargetAdjustmentFactor: 4,             // 25% less, 400% more
		// Consensus rule change deployments.
		//
		// The miner confirmation window is defined as:
		//   target proof of work timespan / target proof of work spacing
		RuleChangeActivationThreshold: 75, // 75% of MinerConfirmationWindow
		MinerConfirmationWindow:       100,
		Forfeit: forfeitureContract{
			Contract: [21]byte{0x88, 0xeb, 0xa5, 0x7d, 0xba, 0x8e, 0x88, 0x3e, 0x96, 0x2b,
				0x1f, 0x13, 0xe7, 0xb0, 0xf3, 0x7f, 0x6d, 0x3b, 0x48, 0x48, 0xfc},
			Opening: [4]byte{0x7c, 0xef, 0x8a, 0x73},
			Filing:  [4]byte{0xb2, 0x18, 0x16, 0x5a},
			Claim:   [4]byte{0x44, 0x90, 0x02, 0xf8},
		},
		ViolationReportDeadline: wire.DESIRABLE_MINER_CANDIDATES + 10,
		ChainID:                 DefaultChainID, // Omega
		RpcPort:                 "7789",
	},

	GenesisHash:      TestNet3GenesisHash[uint32(common.TestNet)],
	GenesisMinerHash: TestNet3GenesisMinerHash[uint32(common.TestNet)],
	PowLimit:         testNet3PowLimit,

	ParentChainId: DefaultParentChainID,
	// Chain parameters
	GenesisBlock:         TestNet3GenesisBlock[uint32(common.TestNet)],
	GenesisMinerBlock:    TestNet3GenesisMinerBlock[uint32(common.TestNet)],
	ChainCurrentStd:      time.Hour * 24000,
	MinBorderFee:         100,
	MinContractDeployFee: 1e3,
	//	ReduceMinDifficulty:      true,
	GenerateSupported: true,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: []Checkpoint{},

	Deployments: []ConsensusDeployment{
		DeploymentTestDummy: {
			PrevVersion: 0,
			FeatureMask: 0,
			StartTime:   1199145601, // January 1, 2008 UTC
			ExpireTime:  1757289600, // Sept 8, 2025 UTC
		},
	},

	// Mempool parameters
	// RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	// Bech32HRPSegwit: "tb", // always tb for test net

	// Address encoding magics
	PubKeyHashAddrID: 0x6f, // starts with m or n
	MultiSigAddrID:   0x67,
	MultiSigAddrXID:  0xC3,

	ScriptHashAddrID: 0xc4, // starts with 2
	ScriptAddrID:     0x13,
	ContractAddrID:   0x88, // start with 8
	PrivateKeyID:     0xef, // starts with 9 (uncompressed) or c (compressed)

	// HDPublicKeyID:  [4]byte{0x04, 0x35, 0x83, 0x94},
	// HDPrivateKeyID: [4]byte{0x04, 0x35, 0x87, 0xcf},

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	// HDCoinType: 1,

	ContractExecLimit: 10000,
	ContractExecFee:   1,
	ContractReqExp:    false,
}

// SimNetParams defines the network parameters for the simulation test Bitcoin
// network.  This network is similar to the normal test network except it is
// intended for private use within a group of individuals doing simulation
// testing.  The functionality is intended to differ in that the only nodes
// which are specifically specified are used to create the network rather than
// following normal discovery rules.  This is important as otherwise it would
// just turn into another public testnet.
/*
var SimNetParams = Params{
	GlobalParams: GlobalParams{
		Name:                     "simnet",
		Net:                      common.SimNet,
		DefaultPort:              "18585",
		DNSSeeds:                 []DNSSeed{}, // NOTE: There must NOT be any seeds.
		PowLimitBits:             0x207fffff,
		CoinbaseMaturity:         100 * wire.MINER_RORATE_FREQ,
		SubsidyReductionInterval: 210000 * wire.MINER_RORATE_FREQ,
		MinimalAward:             0,
		TargetTimespan:           time.Hour * 24 * 14, // 14 days
		TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
		RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
		MinDiffReductionTime:     time.Minute * 20,    // TargetTimePerBlock * 2
		// Consensus rule change deployments.
		//
		// The miner confirmation window is defined as:
		//   target proof of work timespan / target proof of work spacing
		RuleChangeActivationThreshold: 75, // 75% of MinerConfirmationWindow
		MinerConfirmationWindow:       100,
		Forfeit: forfeitureContract{
			Contract: [21]byte{0x88, 0xeb, 0xa5, 0x7d, 0xba, 0x8e, 0x88, 0x3e, 0x96, 0x2b,
				0x1f, 0x13, 0xe7, 0xb0, 0xf3, 0x7f, 0x6d, 0x3b, 0x48, 0x48, 0xfc},
			Opening: [4]byte{0x7c, 0xef, 0x8a, 0x73},
			Filing:  [4]byte{0xb2, 0x18, 0x16, 0x5a},
			Claim:   [4]byte{0x44, 0x90, 0x02, 0xf8},
		},
		ViolationReportDeadline: 10,
		ChainID:                 DefaultChainID, // Omega
		RpcPort:                 "18840",
	},

	GenesisHash:      &simNetGenesisHash,
	GenesisMinerHash: &simNetGenesisMinerHash,
	PowLimit:         simNetPowLimit,

	ParentChainId: DefaultParentChainID,
	// Chain parameters
	GenesisBlock:      &simNetGenesisBlock,
	GenesisMinerBlock: &simNetGenesisMinerBlock,
	ChainCurrentStd:   time.Hour * 24000,
	MinBorderFee:      100000,
	//	ReduceMinDifficulty:      true,
	GenerateSupported: true,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: nil,

	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			PrevVersion: 0,
			FeatureMask: 0,
			StartTime:   0,             // Always available for vote
			ExpireTime:  math.MaxInt64, // Never expires
		},
	},

	// Mempool parameters
	RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "sb", // always sb for sim net

	// Address encoding magics
	PubKeyHashAddrID: 0x3f, // starts with S
	MultiSigAddrID:   0x60,
	MultiSigAddrXID:  0xC3,

	ScriptHashAddrID: 0x7b, // starts with s
	ScriptAddrID:     0x13,
	ContractAddrID:   0x88, // start with 8
	PrivateKeyID:     0x64, // starts with 4 (uncompressed) or F (compressed)

	HDPublicKeyID:  [4]byte{0x04, 0x20, 0xb9, 0x00},
	HDPrivateKeyID: [4]byte{0x04, 0x35, 0xbd, 0x3a},

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 115, // ASCII for s

	ContractExecLimit: 10000,
	ContractExecFee:   1,
	ContractReqExp:    false,
}
*/

var (
	// ErrDuplicateNet describes an error where the parameters for a Bitcoin
	// network could not be set due to the network already being a standard
	// network or previously-registered into this package.
	ErrDuplicateNet = errors.New("duplicate Bitcoin network")

	// ErrUnknownHDKeyID describes an error where the provided id which
	// is intended to identify the network for a hierarchical deterministic
	// private extended key is not registered.
	ErrUnknownHDKeyID = errors.New("unknown hd private extended key bytes")
)

var (
	registeredNets    = make(map[uint32]struct{})
	pubKeyHashAddrIDs = make(map[byte]struct{})
	multisigAddrIDs   = make(map[byte]struct{})
	contractAddrIDs   = make(map[byte]struct{})
	scriptHashAddrIDs = make(map[byte]struct{})

	hdPrivToPubKeyIDs = make(map[[4]byte][]byte)
)

// String returns the hostname of the DNS seed in human-readable form.
func (d DNSSeed) String() string {
	return d.Host
}

// Register registers the network parameters for a Bitcoin network.  This may
// error with ErrDuplicateNet if the network is already registered (either
// due to a previous Register call, or the network being one of the default
// networks).
//
// Network parameters should be registered into this package by a main package
// as early as possible.  Then, library packages may lookup networks or network
// parameters based on inputs and work regardless of the network being standard
// or not.
func Register(params *Params) error {
	if _, ok := registeredNets[params.Net]; ok {
		return ErrDuplicateNet
	}
	registeredNets[params.Net] = struct{}{}
	pubKeyHashAddrIDs[params.PubKeyHashAddrID] = struct{}{}
	multisigAddrIDs[params.MultiSigAddrID] = struct{}{}
	contractAddrIDs[params.ContractAddrID] = struct{}{}
	scriptHashAddrIDs[params.ScriptHashAddrID] = struct{}{}
	// hdPrivToPubKeyIDs[params.HDPrivateKeyID] = params.HDPublicKeyID[:]

	return nil
}

// mustRegister performs the same function as Register except it panics if there
// is an error.  This should only be called from package init functions.
func mustRegister(params *Params) {
	if err := Register(params); err != nil {
		panic("failed to register network: " + err.Error())
	}
}

// IsPubKeyHashAddrID returns whether the id is an identifier known to prefix a
// pay-to-pubkey-hash address on any default or registered network.  This is
// used when decoding an address string into a specific address type.  It is up
// to the caller to check both this and IsScriptHashAddrID and decide whether an
// address is a pubkey hash address, script hash address, neither, or
// undeterminable (if both return true).
func IsPubKeyHashAddrID(id byte) bool {
	_, ok := pubKeyHashAddrIDs[id]
	return ok
}

// IsScriptHashAddrID returns whether the id is an identifier known to prefix a
// pay-to-script-hash address on any default or registered network.  This is
// used when decoding an address string into a specific address type.  It is up
// to the caller to check both this and IsPubKeyHashAddrID and decide whether an
// address is a pubkey hash address, script hash address, neither, or
// undeterminable (if both return true).
func IsScriptHashAddrID(id byte) bool {
	_, ok := scriptHashAddrIDs[id]
	return ok
}

func IsMultiSigAddrID(id byte) bool {
	_, ok := multisigAddrIDs[id]
	return ok
}

// IsContractAddrID returns whether the id is an contract address known to prefix a
// pay-to-contract address on any default or registered network.  This is
// used when decoding an address string into a specific address type.
func IsContractAddrID(id byte) bool {
	_, ok := contractAddrIDs[id]
	return ok
}

// HDPrivateKeyToPublicKeyID accepts a private hierarchical deterministic
// extended key id and returns the associated public key id.  When the provided
// id is not registered, the ErrUnknownHDKeyID error will be returned.
func HDPrivateKeyToPublicKeyID(id []byte) ([]byte, error) {
	if len(id) != 4 {
		return nil, ErrUnknownHDKeyID
	}

	var key [4]byte
	copy(key[:], id)
	pubBytes, ok := hdPrivToPubKeyIDs[key]
	if !ok {
		return nil, ErrUnknownHDKeyID
	}

	return pubBytes, nil
}

// newHashFromStr converts the passed big-endian hex string into a
// chainhash.Hash.  It only differs from the one available in chainhash in that
// it panics on an error since it will only (and must only) be called with
// hard-coded, and therefore known good, hashes.
func newHashFromStr(hexStr string) *chainhash.Hash {
	hash, err := chainhash.NewHashFromStr(hexStr)
	if err != nil {
		// Ordinarily I don't like panics in library code since it
		// can take applications down without them having a chance to
		// recover which is extremely annoying, however an exception is
		// being made in this case because the only way this can panic
		// is if there is an error in the hard-coded hashes.  Thus it
		// will only ever potentially panic on init and therefore is
		// 100% predictable.
		panic(err)
	}
	return hash
}

func init() {
	// Register all default networks when the package is initialized.
	mustRegister(&MainNetParams)
	mustRegister(&TestNet3Params)
	//	mustRegister(&RegressionNetParams)
	//	mustRegister(&SimNetParams)
}

var ActiveNetParams *Params
