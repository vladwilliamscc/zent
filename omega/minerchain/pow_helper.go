package minerchain

import (
	"math/big"

	"btcd/blockchain"
	"btcd/wire"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
)

type powDecisionTrace struct {
	Hash                 chainhash.Hash
	HashNum              *big.Int
	BaseTarget           *big.Int
	RhsTargetBeforeClamp *big.Int
	RhsTargetAfterClamp  *big.Int
	CmpFactor            *big.Int
	PowLimitCmp          int
	ExpectedHit          bool
}

func TryMinerNonce(header *wire.MingingRightBlock, nonce int32, bits uint32, factorPOW int64, h int64, powLimit *big.Int) bool {
	_, hit := traceMinerNonceDecision(header, nonce, bits, factorPOW, h, powLimit)
	return hit
}

func traceMinerNonceDecision(header *wire.MingingRightBlock, nonce int32, bits uint32, factorPOW int64, h int64, powLimit *big.Int) (powDecisionTrace, bool) {
	var trace powDecisionTrace
	if header == nil || powLimit == nil || powLimit.Sign() <= 0 || h < 1 || factorPOW == 0 {
		return trace, false
	}

	localHeader := *header
	localHeader.Bits = bits
	localHeader.Nonce = nonce

	hash := localHeader.BlockHash()
	hashNum := blockchain.HashToBig(&hash)
	baseTarget := blockchain.CompactToBig(bits)
	if baseTarget.Sign() <= 0 {
		trace.Hash = hash
		trace.HashNum = new(big.Int).Set(hashNum)
		trace.BaseTarget = new(big.Int).Set(baseTarget)
		trace.PowLimitCmp = hashNum.Cmp(powLimit)
		return trace, false
	}

	rhsTarget := new(big.Int).Mul(new(big.Int).Set(baseTarget), big.NewInt(h))
	cmpFactor := big.NewInt(1)
	if factorPOW > 0 {
		cmpFactor.SetInt64(factorPOW)
	} else {
		factor := new(big.Int).SetInt64(factorPOW)
		rhsTarget.Mul(rhsTarget, factor.Abs(factor))
	}

	rhsTargetBeforeClamp := new(big.Int).Set(rhsTarget)
	rhsTargetAfterClamp := new(big.Int).Set(rhsTargetBeforeClamp)
	if rhsTargetAfterClamp.Cmp(powLimit) > 0 {
		rhsTargetAfterClamp = new(big.Int).Set(powLimit)
	}

	powLimitCmp := hashNum.Cmp(powLimit)
	expectedHit := false
	if powLimitCmp <= 0 {
		lhs := new(big.Int).Mul(new(big.Int).Set(hashNum), cmpFactor)
		expectedHit = lhs.Cmp(rhsTargetAfterClamp) <= 0
	}

	trace = powDecisionTrace{
		Hash:                 hash,
		HashNum:              new(big.Int).Set(hashNum),
		BaseTarget:           new(big.Int).Set(baseTarget),
		RhsTargetBeforeClamp: new(big.Int).Set(rhsTargetBeforeClamp),
		RhsTargetAfterClamp:  new(big.Int).Set(rhsTargetAfterClamp),
		CmpFactor:            new(big.Int).Set(cmpFactor),
		PowLimitCmp:          powLimitCmp,
		ExpectedHit:          expectedHit,
	}

	return trace, expectedHit
}
