// Copyright (c) 2014-2017 The btcsuite developers
// Copyright (c) 2018-2021 The Omegasuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// NOTE: This file is intended to house the RPC commands that are supported by
// a chain server.

package btcjson

import (
	"btcd/wire"
	"btcutil"
	"encoding/json"
	"fmt"
	"github.com/goinggo/mapstructure"
	"github.com/omegasuite/btcd/chaincfg/chainhash"
	"omega/token"
)

// AddNodeSubCmd defines the type used in the addnode JSON-RPC command for the
// sub command field.
type AddNodeSubCmd string

const (
	// ANAdd indicates the specified host should be added as a persistent
	// peer.
	ANAdd AddNodeSubCmd = "add"

	// ANRemove indicates the specified peer should be removed.
	ANRemove AddNodeSubCmd = "remove"

	// ANOneTry indicates the specified host should try to connect once,
	// but it should not be made persistent.
	ANOneTry AddNodeSubCmd = "onetry"
)

// AddNodeCmd defines the addnode JSON-RPC command.
type AddNodeCmd struct {
	Addr   string
	SubCmd AddNodeSubCmd `jsonrpcusage:"\"add|remove|onetry\""`
}

// NewAddNodeCmd returns a new instance which can be used to issue an addnode
// JSON-RPC command.
func NewAddNodeCmd(addr string, subCmd AddNodeSubCmd) *AddNodeCmd {
	return &AddNodeCmd{
		Addr:   addr,
		SubCmd: subCmd,
	}
}

// TransactionInput represents the inputs to a transaction.  Specifically a
// transaction hash and output number pair.
type TransactionInput struct {
	Txid string `json:"txid"`
	Vout uint32 `json:"vout"`
}

// Definition represents the definitions to a transaction.  Specifically a
// transaction hash and output number pair. We use this strange structure
// to emulate interface-json conversion
type Definition struct {
	DefType uint32                 `json:"deftype"`
	DefData map[string]interface{} `json:"data"`
}

type Vertex struct {
	Lat int32 `json:"lat"`
	Lng int32 `json:"lng"`
	Alt int32 `json:"alt"`
}

type Border struct {
	Father string `json:"father"`
	Begin  Vertex `json:"begin"`
	End    Vertex `json:"end"`
}

type Polygon struct {
	Loops [][]string `json:"loops"`
}

type Right struct {
	Father string `json:"father"`
	Desc   string `json:"desc"`
	Attrib byte   `json:"attrib"`
}

func (t *Definition) ConvertTo() token.Definition {
	switch t.DefType {
	case token.DefTypeVertex:
		v := Vertex{}
		if err := mapstructure.Decode(t.DefData, &v); err != nil {
			return nil
		}
		t := token.VertexDef{}
		t.SetLat(v.Lat)
		t.SetLng(v.Lng)
		return &t

	case token.DefTypeBorder:
		b := Border{}
		if err := mapstructure.Decode(t.DefData, &b); err != nil {
			return nil
		}
		f, err := chainhash.NewHashFromStr(b.Father)
		if err != nil {
			f = &chainhash.Hash{}
			copy(f[:], b.Father[:])
		}
		bd := &token.BorderDef{
			Father: *f,
		}
		bd.Begin.SetLat(b.Begin.Lat)
		bd.Begin.SetLng(b.Begin.Lng)
		bd.Begin.SetAlt(b.Begin.Alt)
		bd.End.SetLat(b.End.Lat)
		bd.End.SetLng(b.End.Lng)
		bd.End.SetAlt(b.End.Alt)
		if bd.Begin.IsEqual(&bd.End) {
			return nil
		}
		return bd

	case token.DefTypePolygon:
		p := Polygon{}
		if err := mapstructure.Decode(t.DefData, &p); err != nil {
			return nil
		}
		t := token.PolygonDef{
			Loops: make([]token.LoopDef, 0, len(p.Loops)),
		}
		for _, loop := range p.Loops {
			l := make([]chainhash.Hash, 0, len(loop))
			for _, b := range loop {
				h, err := chainhash.NewHashFromStr(b)
				if err != nil {
					h = &chainhash.Hash{}
					copy(h[:], b[:])
				}
				l = append(l, *h)
			}
			t.Loops = append(t.Loops, l)
		}
		return &t

	case token.DefTypeRight:
		r := Right{}
		if err := mapstructure.Decode(t.DefData, &r); err != nil {
			return nil
		}
		f, err := chainhash.NewHashFromStr(r.Father)
		if err != nil {
			f = &chainhash.Hash{}
			copy(f[:], r.Father[:])
		}
		t := token.RightDef{
			Father: *f,
			Desc:   make([]byte, len(r.Desc)),
			Attrib: r.Attrib,
		}
		copy(t.Desc[:], r.Desc[:])
		return &t
	}
	return nil
}

// Token represents a token. A transferrable value.
type Token struct {
	TokenType uint64                 `json:"tokentype"`
	Value     map[string]interface{} `json:"value"`  // either an numeric amount or a hash value
	Rights    string                 `json:"rights"` // hash of rights
	Script    *string                `json:"script"` // script to use
}

func (t *Token) ConvertTo(mtx *wire.MsgTx) *wire.TxOut {
	v := wire.TxOut{}
	v.TokenType = t.TokenType
	v.Rights, _ = chainhash.NewHashFromStr(t.Rights)

	if t.TokenType&1 == 0 {
		if i, ok := t.Value["val"]; ok {
			if f, ok := i.(float64); ok {
				nm, _ := btcutil.NewAmount(f, t.TokenType)
				v.Value = &token.NumToken{Val: int64(nm)}
				return &v
			}
		}
		return nil
	} else {
		if hh, ok := t.Value["Hash"]; ok {
			s := hh.(string)
			h, _ := chainhash.NewHashFromStr(s)
			if h == nil {
				h = &chainhash.Hash{}
				copy(h[:], s[:])
			}
			v.Value = &token.HashToken{Hash: *h}
		} else {
			return nil
		}
	}

	return &v
}

// CreateRawTransactionCmd defines the createrawtransaction JSON-RPC command.
type ParseRawTransactionCmd struct {
	RawTx string
}

// NewParseRawTransactionCmd returns a new instance which can be used to issue
// a createrawtransaction JSON-RPC command.
//
// Amounts are in OMC.
func NewParseRawTransactionCmd(s string) *ParseRawTransactionCmd {
	return &ParseRawTransactionCmd{
		RawTx: s,
	}
}

// CreateRawTransactionCmd defines the createrawtransaction JSON-RPC command.
type CreateRawTransactionCmd struct {
	Inputs       []TransactionInput
	Definitions  []Definition
	Amounts      []map[string]Token `jsonrpcusage:"{\"address\":token,...}"`
	LockTime     *int64
	Targetchains *[]uint32
}

// NewCreateRawTransactionCmd returns a new instance which can be used to issue
// a createrawtransaction JSON-RPC command.
//
// Amounts are in OMC.
func NewCreateRawTransactionCmd(inputs []TransactionInput, defs []Definition, amounts []map[string]Token,
	targetchains *[]uint32, lockTime *int64) *CreateRawTransactionCmd {

	return &CreateRawTransactionCmd{
		Inputs:       inputs,
		Definitions:  defs,
		Amounts:      amounts,
		Targetchains: targetchains,
		LockTime:     lockTime,
	}
}

// DecodeRawTransactionCmd defines the decoderawtransaction JSON-RPC command.
type DecodeRawTransactionCmd struct {
	HexTx string
}

// NewDecodeRawTransactionCmd returns a new instance which can be used to issue
// a decoderawtransaction JSON-RPC command.
func NewDecodeRawTransactionCmd(hexTx string) *DecodeRawTransactionCmd {
	return &DecodeRawTransactionCmd{
		HexTx: hexTx,
	}
}

// DecodeScriptCmd defines the decodescript JSON-RPC command.
type DecodeScriptCmd struct {
	HexScript string
}

// NewDecodeScriptCmd returns a new instance which can be used to issue a
// decodescript JSON-RPC command.
func NewDecodeScriptCmd(hexScript string) *DecodeScriptCmd {
	return &DecodeScriptCmd{
		HexScript: hexScript,
	}
}

// GetAddedNodeInfoCmd defines the getaddednodeinfo JSON-RPC command.
type GetAddedNodeInfoCmd struct {
	DNS  bool
	Node *string
}

type MsgXrossL2 struct {
	Utxo      string
	TokenType int64
	Value     string
	Rights    string
	PkScript  string
	Redeem    string
}

type BTCL2Data struct {
	Hash   string
	Height int32
	Txs    []MsgXrossL2
}

// NewGetAddedNodeInfoCmd returns a new instance which can be used to issue a
// getaddednodeinfo JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetAddedNodeInfoCmd(dns bool, node *string) *GetAddedNodeInfoCmd {
	return &GetAddedNodeInfoCmd{
		DNS:  dns,
		Node: node,
	}
}

// GetBestBlockHashCmd defines the getbestblockhash JSON-RPC command.
type GetBestBlockHashCmd struct{}

type GetBestMinerBlockHashCmd struct{}

// NewGetBestBlockHashCmd returns a new instance which can be used to issue a
// getbestblockhash JSON-RPC command.
func NewGetBestBlockHashCmd() *GetBestBlockHashCmd {
	return &GetBestBlockHashCmd{}
}

func NewGetBestMinerBlockHashCmd() *GetBestMinerBlockHashCmd {
	return &GetBestMinerBlockHashCmd{}
}

// GetBlockCmd defines the getblock JSON-RPC command.
type GetBlockCmd struct {
	Hash      string
	Verbose   *bool `jsonrpcdefault:"true"`
	VerboseTx *bool `jsonrpcdefault:"false"`
}

type SearchBorderCmd struct {
	Left   int32
	Right  int32
	Bottom int32
	Top    int32
	Lod    byte
}

type GetTPSViewCmd struct {
	Address string
}

type ContractCallCmd struct {
	Contract string
	Input    string
}

type GetBlockTxHashesCmd struct {
	Hash string
}

type AddMiningKeyCmd struct {
	KeyType bool
	Key     string
}

type AddCollateralCmd struct {
	Hash  string
	Index uint32
}

type GetMinerRuntimeMetricsCmd struct{}

func NewAddMiningKeyCmd(k string, ktype bool) *AddMiningKeyCmd {
	return &AddMiningKeyCmd{
		KeyType: ktype,
		Key:     k,
	}
}

func NewGetMinerRuntimeMetricsCmd() *GetMinerRuntimeMetricsCmd {
	return &GetMinerRuntimeMetricsCmd{}
}

type GetMinerBlockHeightCmd struct {
	Hash string
}

// NewGetBlockCmd returns a new instance which can be used to issue a getblock
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetBlockHeightCmd(hash string) *GetMinerBlockHeightCmd {
	return &GetMinerBlockHeightCmd{
		Hash: hash,
	}
}

type GetMinerBlockCmd struct {
	Hash    string
	Verbose *bool `jsonrpcdefault:"true"`
}

// NewGetBlockCmd returns a new instance which can be used to issue a getblock
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetBlockCmd(hash string, verbose, verboseTx *bool) *GetBlockCmd {
	return &GetBlockCmd{
		Hash:      hash,
		Verbose:   verbose,
		VerboseTx: verboseTx,
	}
}

func NewGetBlockTxHasesCmd(hash string) *GetBlockTxHashesCmd {
	return &GetBlockTxHashesCmd{
		Hash: hash,
	}
}

func NewContractCallCmd(contract, input string) *ContractCallCmd {
	return &ContractCallCmd{
		Contract: contract,
		Input:    input,
	}
}

func NewSearchBorderCmd(left, right, bottom, top int32, lod byte) *SearchBorderCmd {
	return &SearchBorderCmd{
		Left:   left,
		Right:  right,
		Bottom: bottom,
		Top:    top,
		Lod:    lod,
	}
}

func NewGetMinerBlockCmd(hash string, verbose *bool) *GetMinerBlockCmd {
	return &GetMinerBlockCmd{
		Hash:    hash,
		Verbose: verbose,
	}
}

// GetBlockChainInfoCmd defines the getblockchaininfo JSON-RPC command.
type GetBlockChainInfoCmd struct{}

// NewGetBlockChainInfoCmd returns a new instance which can be used to issue a
// getblockchaininfo JSON-RPC command.
func NewGetBlockChainInfoCmd() *GetBlockChainInfoCmd {
	return &GetBlockChainInfoCmd{}
}

// GetBlockCountCmd defines the getblockcount JSON-RPC command.
type GetBlockCountCmd struct{}
type GetMinerBlockCountCmd struct{}

// NewGetBlockCountCmd returns a new instance which can be used to issue a
// getblockcount JSON-RPC command.
func NewGetBlockCountCmd() *GetBlockCountCmd {
	return &GetBlockCountCmd{}
}

func NewGetMinerBlockCountCmd() *GetMinerBlockCountCmd {
	return &GetMinerBlockCountCmd{}
}

// GetBlockHashCmd defines the getblockhash JSON-RPC command.
type GetBlockHashCmd struct {
	Index int64
}

type GetMinerBlockHashCmd struct {
	Index int64
}

// NewGetBlockHashCmd returns a new instance which can be used to issue a
// getblockhash JSON-RPC command.
func NewGetBlockHashCmd(index int64) *GetBlockHashCmd {
	return &GetBlockHashCmd{
		Index: index,
	}
}

func NewGetMinerBlockHashCmd(index int64) *GetMinerBlockHashCmd {
	return &GetMinerBlockHashCmd{
		Index: index,
	}
}

// GetBlockHeaderCmd defines the getblockheader JSON-RPC command.
type GetBlockHeaderCmd struct {
	Hash    string
	Verbose *bool `jsonrpcdefault:"true"`
}

// NewGetBlockHeaderCmd returns a new instance which can be used to issue a
// getblockheader JSON-RPC command.
func NewGetBlockHeaderCmd(hash string, verbose *bool) *GetBlockHeaderCmd {
	return &GetBlockHeaderCmd{
		Hash:    hash,
		Verbose: verbose,
	}
}

// TemplateRequest is a request object as defined in BIP22
// (https://en.bitcoin.it/wiki/BIP_0022), it is optionally provided as an
// pointer argument to GetBlockTemplateCmd.
type TemplateRequest struct {
	Mode         string   `json:"mode,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`

	// Optional long polling.
	LongPollID string `json:"longpollid,omitempty"`

	// Optional template tweaking.  SigOpLimit and SizeLimit can be int64
	// or bool.
	SigOpLimit interface{} `json:"sigoplimit,omitempty"`
	SizeLimit  interface{} `json:"sizelimit,omitempty"`
	MaxVersion uint32      `json:"maxversion,omitempty"`

	// Basic pool extension from BIP 0023.
	Target string `json:"target,omitempty"`

	// Block proposal from BIP 0023.  Data is only provided when Mode is
	// "proposal".
	Data   string `json:"data,omitempty"`
	WorkID string `json:"workid,omitempty"`
}

// convertTemplateRequestField potentially converts the provided value as
// needed.
func convertTemplateRequestField(fieldName string, iface interface{}) (interface{}, error) {
	switch val := iface.(type) {
	case nil:
		return nil, nil
	case bool:
		return val, nil
	case float64:
		if val == float64(int64(val)) {
			return int64(val), nil
		}
	}

	str := fmt.Sprintf("the %s field must be unspecified, a boolean, or "+
		"a 64-bit integer", fieldName)
	return nil, makeError(ErrInvalidType, str)
}

// UnmarshalJSON provides a custom Unmarshal method for TemplateRequest.  This
// is necessary because the SigOpLimit and SizeLimit fields can only be specific
// types.
func (t *TemplateRequest) UnmarshalJSON(data []byte) error {
	type templateRequest TemplateRequest

	request := (*templateRequest)(t)
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}

	// The SigOpLimit field can only be nil, bool, or int64.
	val, err := convertTemplateRequestField("sigoplimit", request.SigOpLimit)
	if err != nil {
		return err
	}
	request.SigOpLimit = val

	// The SizeLimit field can only be nil, bool, or int64.
	val, err = convertTemplateRequestField("sizelimit", request.SizeLimit)
	if err != nil {
		return err
	}
	request.SizeLimit = val

	return nil
}

// GetBlockTemplateCmd defines the getblocktemplate JSON-RPC command.
type GetBlockTemplateCmd struct {
	Request *TemplateRequest
}

// NewGetBlockTemplateCmd returns a new instance which can be used to issue a
// getblocktemplate JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetBlockTemplateCmd(request *TemplateRequest) *GetBlockTemplateCmd {
	return &GetBlockTemplateCmd{
		Request: request,
	}
}

// GetCFilterCmd defines the getcfilter JSON-RPC command.
type GetCFilterCmd struct {
	Hash       string
	FilterType wire.FilterType
}

// NewGetCFilterCmd returns a new instance which can be used to issue a
// getcfilter JSON-RPC command.
func NewGetCFilterCmd(hash string, filterType wire.FilterType) *GetCFilterCmd {
	return &GetCFilterCmd{
		Hash:       hash,
		FilterType: filterType,
	}
}

// GetCFilterHeaderCmd defines the getcfilterheader JSON-RPC command.
type GetCFilterHeaderCmd struct {
	Hash       string
	FilterType wire.FilterType
}

// NewGetCFilterHeaderCmd returns a new instance which can be used to issue a
// getcfilterheader JSON-RPC command.
func NewGetCFilterHeaderCmd(hash string,
	filterType wire.FilterType) *GetCFilterHeaderCmd {
	return &GetCFilterHeaderCmd{
		Hash:       hash,
		FilterType: filterType,
	}
}

// GetChainTipsCmd defines the getchaintips JSON-RPC command.
type GetChainTipsCmd struct{}

// NewGetChainTipsCmd returns a new instance which can be used to issue a
// getchaintips JSON-RPC command.
func NewGetChainTipsCmd() *GetChainTipsCmd {
	return &GetChainTipsCmd{}
}

// GetConnectionCountCmd defines the getconnectioncount JSON-RPC command.
type GetConnectionCountCmd struct{}

// ResetConnectionCmd defines the resetconnection JSON-RPC command.
type ResetConnectionCmd struct{}

// NewGetConnectionCountCmd returns a new instance which can be used to issue a
// getconnectioncount JSON-RPC command.
func NewGetConnectionCountCmd() *GetConnectionCountCmd {
	return &GetConnectionCountCmd{}
}

// GetDifficultyCmd defines the getdifficulty JSON-RPC command.
type GetDifficultyCmd struct{}

// NewGetDifficultyCmd returns a new instance which can be used to issue a
// getdifficulty JSON-RPC command.
func NewGetDifficultyCmd() *GetDifficultyCmd {
	return &GetDifficultyCmd{}
}

// GetGenerateCmd defines the getgenerate JSON-RPC command.
type GetGenerateCmd struct{}

// NewGetGenerateCmd returns a new instance which can be used to issue a
// getgenerate JSON-RPC command.
func NewGetGenerateCmd() *GetGenerateCmd {
	return &GetGenerateCmd{}
}

// GetHashesPerSecCmd defines the gethashespersec JSON-RPC command.
type GetHashesPerSecCmd struct{}

// NewGetHashesPerSecCmd returns a new instance which can be used to issue a
// gethashespersec JSON-RPC command.
func NewGetHashesPerSecCmd() *GetHashesPerSecCmd {
	return &GetHashesPerSecCmd{}
}

// GetInfoCmd defines the getinfo JSON-RPC command.
type GetInfoCmd struct{}

// NewGetInfoCmd returns a new instance which can be used to issue a
// getinfo JSON-RPC command.
func NewGetInfoCmd() *GetInfoCmd {
	return &GetInfoCmd{}
}

// GetMempoolEntryCmd defines the getmempoolentry JSON-RPC command.
type GetMempoolEntryCmd struct {
	TxID string
}

// NewGetMempoolEntryCmd returns a new instance which can be used to issue a
// getmempoolentry JSON-RPC command.
func NewGetMempoolEntryCmd(txHash string) *GetMempoolEntryCmd {
	return &GetMempoolEntryCmd{
		TxID: txHash,
	}
}

// GetMempoolInfoCmd defines the getmempoolinfo JSON-RPC command.
type GetMempoolInfoCmd struct{}

// NewGetMempoolInfoCmd returns a new instance which can be used to issue a
// getmempool JSON-RPC command.
func NewGetMempoolInfoCmd() *GetMempoolInfoCmd {
	return &GetMempoolInfoCmd{}
}

// GetMiningInfoCmd defines the getmininginfo JSON-RPC command.
type GetMiningInfoCmd struct{}

// NewGetMiningInfoCmd returns a new instance which can be used to issue a
// getmininginfo JSON-RPC command.
func NewGetMiningInfoCmd() *GetMiningInfoCmd {
	return &GetMiningInfoCmd{}
}

// GetNetworkInfoCmd defines the getnetworkinfo JSON-RPC command.
type GetNetworkInfoCmd struct{}

// NewGetNetworkInfoCmd returns a new instance which can be used to issue a
// getnetworkinfo JSON-RPC command.
func NewGetNetworkInfoCmd() *GetNetworkInfoCmd {
	return &GetNetworkInfoCmd{}
}

// GetNetTotalsCmd defines the getnettotals JSON-RPC command.
type GetNetTotalsCmd struct{}

// NewGetNetTotalsCmd returns a new instance which can be used to issue a
// getnettotals JSON-RPC command.
func NewGetNetTotalsCmd() *GetNetTotalsCmd {
	return &GetNetTotalsCmd{}
}

// GetNetworkHashPSCmd defines the getnetworkhashps JSON-RPC command.
type GetNetworkHashPSCmd struct {
	Blocks *int `jsonrpcdefault:"120"`
	Height *int `jsonrpcdefault:"-1"`
}

// NewGetNetworkHashPSCmd returns a new instance which can be used to issue a
// getnetworkhashps JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetNetworkHashPSCmd(numBlocks, height *int) *GetNetworkHashPSCmd {
	return &GetNetworkHashPSCmd{
		Blocks: numBlocks,
		Height: height,
	}
}

// GetPeerInfoCmd defines the getpeerinfo JSON-RPC command.
type GetPeerInfoCmd struct{}

// NewGetPeerInfoCmd returns a new instance which can be used to issue a getpeer
// JSON-RPC command.
func NewGetPeerInfoCmd() *GetPeerInfoCmd {
	return &GetPeerInfoCmd{}
}

// GetRawMempoolCmd defines the getmempool JSON-RPC command.
type GetRawMempoolCmd struct {
	Verbose *bool `jsonrpcdefault:"false"`
}

// GetIssuedTokensCmd defines the getissuedtokens JSON-RPC command.
type GetIssuedTokensCmd struct {
	Start *uint32 `jsonrpcdefault:"0"`
	Count *uint32 `jsonrpcdefault:"100"`
}

type GetCrossChainDBCmd struct {
	Clear uint32
}

// Createxferl2txoCmd defines the ceatexferl2txo JSON-RPC command.
type Createxferl2txoCmd struct {
	Amount    uint64
	Address   [20]byte
	AssetType *uint32 `jsonrpcdefault:"0"`
}

func NewCreatexferl2txoCmd(addr [20]byte) *Createxferl2txoCmd {
	return &Createxferl2txoCmd{
		Amount:  0,
		Address: addr,
	}
}

type GetTreasuryCmd struct {
}
type TreasuryAsset struct {
	Typeid   uint64 // type id in L2
	Amount   uint64 //
	Outpoint string
	Owners   []string // signatures required
	Pkscript string
}

type GetSignersCmd struct {
}
type TreasuryPlgAsset struct {
	Utxo     string
	Amount   uint64
	Firstuse uint32 // height when it becomes a signer
}
type TreasurySigners struct {
	Address   string             // Address of signer
	Pubkey    string             // Pubkey of signer
	Voting    uint8              // Voting power in %
	Retiring  bool               // whether is Retiring
	Joined    uint32             // height when it becomes a signer
	Pledged   []TreasuryPlgAsset // Assets Pledged
	Btcprofit uint64             // profits in BTC
}
type GetBtcPoolCmd struct {
}
type GetL2PoolCmd struct {
}

// NewGetRawMempoolCmd returns a new instance which can be used to issue a
// getrawmempool JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetRawMempoolCmd(verbose *bool) *GetRawMempoolCmd {
	return &GetRawMempoolCmd{
		Verbose: verbose,
	}
}

// GetRawTransactionCmd defines the getrawtransaction JSON-RPC command.
//
// NOTE: This field is an int versus a bool to remain compatible with Bitcoin
// Core even though it really should be a bool.
type GetRawTransactionCmd struct {
	Txid           string
	Verbose        *int  `jsonrpcdefault:"0"`
	IncludeMempool *bool `jsonrpcdefault:"true"`
	InMainChain    *bool `jsonrpcdefault:"true"`
}

// NewGetRawTransactionCmd returns a new instance which can be used to issue a
// getrawtransaction JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetRawTransactionCmd(txHash string, verbose *int, includeMempool *bool) *GetRawTransactionCmd {
	return &GetRawTransactionCmd{
		Txid:           txHash,
		Verbose:        verbose,
		IncludeMempool: includeMempool,
	}
}

// GetTxOutCmd defines the gettxout JSON-RPC command.
type GetTxOutCmd struct {
	Txid           string
	Vout           uint32
	IncludeMempool *bool `jsonrpcdefault:"true"`
	IncludeLocked  *bool `jsonrpcdefault:"true"`
}

// NewGetTxOutCmd returns a new instance which can be used to issue a gettxout
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetTxOutCmd(txHash string, vout uint32, includeMempool *bool, IncludeLocked *bool) *GetTxOutCmd {
	return &GetTxOutCmd{
		Txid:           txHash,
		Vout:           vout,
		IncludeMempool: includeMempool,
		IncludeLocked:  IncludeLocked,
	}
}

// ListUtxosCmd defines the ListUtxos JSON-RPC command.
type ListUtxosCmd struct {
	Begin     *int32
	Run       *uint32
	Minval    *int64
	Tokentype *uint64
	Address   *string
}

// NewListUtxosCmd returns a new instance which can be used to issue a ListUtxos
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListUtxosCmd(begin *int32, run *uint32, m *int64) *ListUtxosCmd {
	return &ListUtxosCmd{
		Begin:  begin,
		Run:    run,
		Minval: m,
	}
}

// GetDefineCmd defines the GetDefine JSON-RPC command.
type GetDefineCmd struct {
	Kind      uint32
	Hash      string
	Recursive bool
}

// NewGetTxOutCmd returns a new instance which can be used to issue a gettxout
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetDefineCmd(kind uint32, hash string, recursive bool) *GetDefineCmd {
	return &GetDefineCmd{
		Kind:      kind,
		Hash:      hash,
		Recursive: recursive,
	}
}

// GetTxOutProofCmd defines the gettxoutproof JSON-RPC command.
type GetTxOutProofCmd struct {
	TxIDs     []string
	BlockHash *string
}

// NewGetTxOutProofCmd returns a new instance which can be used to issue a
// gettxoutproof JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetTxOutProofCmd(txIDs []string, blockHash *string) *GetTxOutProofCmd {
	return &GetTxOutProofCmd{
		TxIDs:     txIDs,
		BlockHash: blockHash,
	}
}

// GetTxOutSetInfoCmd defines the gettxoutsetinfo JSON-RPC command.
type GetTxOutSetInfoCmd struct{}

// NewGetTxOutSetInfoCmd returns a new instance which can be used to issue a
// gettxoutsetinfo JSON-RPC command.
func NewGetTxOutSetInfoCmd() *GetTxOutSetInfoCmd {
	return &GetTxOutSetInfoCmd{}
}

// GetWorkCmd defines the getwork JSON-RPC command.
type GetWorkCmd struct {
	Data *string
}

// NewGetWorkCmd returns a new instance which can be used to issue a getwork
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetWorkCmd(data *string) *GetWorkCmd {
	return &GetWorkCmd{
		Data: data,
	}
}

// HelpCmd defines the help JSON-RPC command.
type HelpCmd struct {
	Command *string
}

// NewHelpCmd returns a new instance which can be used to issue a help JSON-RPC
// command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewHelpCmd(command *string) *HelpCmd {
	return &HelpCmd{
		Command: command,
	}
}

// InvalidateBlockCmd defines the invalidateblock JSON-RPC command.
type InvalidateBlockCmd struct {
	BlockHash string
}

// NewInvalidateBlockCmd returns a new instance which can be used to issue a
// invalidateblock JSON-RPC command.
func NewInvalidateBlockCmd(blockHash string) *InvalidateBlockCmd {
	return &InvalidateBlockCmd{
		BlockHash: blockHash,
	}
}

// PingCmd defines the ping JSON-RPC command.
type PingCmd struct{}

// NewPingCmd returns a new instance which can be used to issue a ping JSON-RPC
// command.
func NewPingCmd() *PingCmd {
	return &PingCmd{}
}

// PreciousBlockCmd defines the preciousblock JSON-RPC command.
type PreciousBlockCmd struct {
	BlockHash string
}

// NewPreciousBlockCmd returns a new instance which can be used to issue a
// preciousblock JSON-RPC command.
func NewPreciousBlockCmd(blockHash string) *PreciousBlockCmd {
	return &PreciousBlockCmd{
		BlockHash: blockHash,
	}
}

// ReconsiderBlockCmd defines the reconsiderblock JSON-RPC command.
type ReconsiderBlockCmd struct {
	BlockHash string
}

// NewReconsiderBlockCmd returns a new instance which can be used to issue a
// reconsiderblock JSON-RPC command.
func NewReconsiderBlockCmd(blockHash string) *ReconsiderBlockCmd {
	return &ReconsiderBlockCmd{
		BlockHash: blockHash,
	}
}

// SearchRawTransactionsCmd defines the searchrawtransactions JSON-RPC command.
type SearchRawTransactionsCmd struct {
	Address     string
	Verbose     *int  `jsonrpcdefault:"1"`
	Skip        *int  `jsonrpcdefault:"0"`
	Count       *int  `jsonrpcdefault:"100"`
	VinExtra    *int  `jsonrpcdefault:"0"`
	Reverse     *bool `jsonrpcdefault:"false"`
	FilterAddrs *[]string
	Signatures  *bool `jsonrpcdefault:"false"`
}

// SearchRawSpendCmd defines the searchspend JSON-RPC command.
type SearchRawSpendCmd struct {
	Address  string
	Spending string
	Skip     *int `jsonrpcdefault:"0"`
}

type VerifySigCmd struct {
	HexTx string
}

// NewSearchRawTransactionsCmd returns a new instance which can be used to issue a
// sendrawtransaction JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSearchRawTransactionsCmd(address string, verbose, skip, count *int, vinExtra *int, reverse *bool, filterAddrs *[]string) *SearchRawTransactionsCmd {
	return &SearchRawTransactionsCmd{
		Address:     address,
		Verbose:     verbose,
		Skip:        skip,
		Count:       count,
		VinExtra:    vinExtra,
		Reverse:     reverse,
		FilterAddrs: filterAddrs,
		Signatures:  nil,
	}
}

func NewVerifySigCmd(hexTx string) *VerifySigCmd {
	return &VerifySigCmd{
		HexTx: hexTx,
	}
}

// TokenAddressCmd defines the tokenaddress JSON-RPC command.
type TokenAddressCmd struct {
	TokenType uint64
}

// NewTokenAddressCmd returns a new instance which can be used to issue a
// TokenAddressCmd JSON-RPC command.
func NewTokenAddressCmd(tokentype uint64) *TokenAddressCmd {
	return &TokenAddressCmd{
		TokenType: tokentype,
	}
}

// TryContractCmd defines the trycontract JSON-RPC command.
type TryContractCmd struct {
	HexTx string
}

// NewTryContractCmd returns a new instance which can be used to issue a
// TryContractCmd JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewTryContractCmd(hexTx string) *TryContractCmd {
	return &TryContractCmd{
		HexTx: hexTx,
	}
}

// SendRawTransactionCmd defines the sendrawtransaction JSON-RPC command.
type RecastRawTransactionCmd struct {
}

// NewRecastRawTransactionCmd returns a new instance which can be used to issue a
// recastrawtransactionCmd JSON-RPC command.
//

func NewRecastRawTransactionCmd() *RecastRawTransactionCmd {
	return &RecastRawTransactionCmd{}
}

// SendRawTransactionCmd defines the sendrawtransaction JSON-RPC command.
type SendRawTransactionCmd struct {
	HexTx         string
	AllowHighFees *bool `jsonrpcdefault:"false"`
	WaitConfirm   *int  `jsonrpcdefault:"0"`
	FulllValidate *bool `jsonrpcdefault:"false"`
}

// NewSendRawTransactionCmd returns a new instance which can be used to issue a
// sendrawtransaction JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSendRawTransactionCmd(hexTx string, allowHighFees *bool) *SendRawTransactionCmd {
	return &SendRawTransactionCmd{
		HexTx:         hexTx,
		AllowHighFees: allowHighFees,
	}
}

type CheckForkCmd struct {
	HexTx string
}

func NewCheckForkCmd(hexTx string) *CheckForkCmd {
	return &CheckForkCmd{
		HexTx: hexTx,
	}
}

// SetGenerateCmd defines the setgenerate JSON-RPC command.
type SetGenerateCmd struct {
	Generate     bool
	IsMiner      bool
	GenProcLimit *int `jsonrpcdefault:"-1"`
}

// NewSetGenerateCmd returns a new instance which can be used to issue a
// setgenerate JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSetGenerateCmd(generate bool, genProcLimit *int) *SetGenerateCmd {
	return &SetGenerateCmd{
		Generate:     generate,
		GenProcLimit: genProcLimit,
	}
}

// ConfirmationsCmd defines the Confirmations JSON-RPC command.
type ConfirmationsCmd struct {
	TxHash string
}

func NewConfirmationsCmd(hash string) *ConfirmationsCmd {
	return &ConfirmationsCmd{
		TxHash: hash,
	}
}

// StopCmd defines the stop JSON-RPC command.
type StopCmd struct{}

// NewStopCmd returns a new instance which can be used to issue a stop JSON-RPC
// command.
func NewStopCmd() *StopCmd {
	return &StopCmd{}
}

// SubmitBlockOptions represents the optional options struct provided with a
// SubmitBlockCmd command.
type SubmitBlockOptions struct {
	// must be provided if server provided a workid with template.
	WorkID string `json:"workid,omitempty"`
}

// SubmitBlockCmd defines the submitblock JSON-RPC command.
type SubmitBlockCmd struct {
	HexBlock string
	Options  *SubmitBlockOptions
}

// NewSubmitBlockCmd returns a new instance which can be used to issue a
// submitblock JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSubmitBlockCmd(hexBlock string, options *SubmitBlockOptions) *SubmitBlockCmd {
	return &SubmitBlockCmd{
		HexBlock: hexBlock,
		Options:  options,
	}
}

// UptimeCmd defines the uptime JSON-RPC command.
type UptimeCmd struct{}

// NewUptimeCmd returns a new instance which can be used to issue an uptime JSON-RPC command.
func NewUptimeCmd() *UptimeCmd {
	return &UptimeCmd{}
}

// ValidateAddressCmd defines the validateaddress JSON-RPC command.
type ValidateAddressCmd struct {
	Address string
}

// NewValidateAddressCmd returns a new instance which can be used to issue a
// validateaddress JSON-RPC command.
func NewValidateAddressCmd(address string) *ValidateAddressCmd {
	return &ValidateAddressCmd{
		Address: address,
	}
}

// ShutdownCmd defines the shutdown JSON-RPC command.
type ShutdownCmd struct {
	NoneCommittee *bool `jsonrpcdefault:"false"`
}

// VerifyMessageCmd defines the verifymessage JSON-RPC command.
type VerifyMessageCmd struct {
	Address   string
	Signature string
	Message   string
}

// NewVerifyMessageCmd returns a new instance which can be used to issue a
// verifymessage JSON-RPC command.
func NewVerifyMessageCmd(address, signature, message string) *VerifyMessageCmd {
	return &VerifyMessageCmd{
		Address:   address,
		Signature: signature,
		Message:   message,
	}
}

// VerifyTxOutProofCmd defines the verifytxoutproof JSON-RPC command.
type VerifyTxOutProofCmd struct {
	Proof string
}

// NewVerifyTxOutProofCmd returns a new instance which can be used to issue a
// verifytxoutproof JSON-RPC command.
func NewVerifyTxOutProofCmd(proof string) *VerifyTxOutProofCmd {
	return &VerifyTxOutProofCmd{
		Proof: proof,
	}
}

func init() {
	// No special flags for commands in this file.
	flags := UsageFlag(0)

	MustRegisterCmd("addnode", (*AddNodeCmd)(nil), flags)
	MustRegisterCmd("createrawtransaction", (*CreateRawTransactionCmd)(nil), flags)
	MustRegisterCmd("crt", (*CreateRawTransactionCmd)(nil), flags)
	MustRegisterCmd("parserawtransaction", (*ParseRawTransactionCmd)(nil), flags)
	MustRegisterCmd("decoderawtransaction", (*DecodeRawTransactionCmd)(nil), flags)
	MustRegisterCmd("decodescript", (*DecodeScriptCmd)(nil), flags)
	MustRegisterCmd("getaddednodeinfo", (*GetAddedNodeInfoCmd)(nil), flags)
	MustRegisterCmd("getbestblockhash", (*GetBestBlockHashCmd)(nil), flags)
	MustRegisterCmd("gbbh", (*GetBestBlockHashCmd)(nil), flags)
	MustRegisterCmd("getbestminerblockhash", (*GetBestMinerBlockHashCmd)(nil), flags)
	MustRegisterCmd("getblock", (*GetBlockCmd)(nil), flags)
	MustRegisterCmd("gbk", (*GetBlockCmd)(nil), flags)
	MustRegisterCmd("getblocktxhashes", (*GetBlockTxHashesCmd)(nil), flags)
	MustRegisterCmd("gbkth", (*GetBlockTxHashesCmd)(nil), flags)
	MustRegisterCmd("searchborder", (*SearchBorderCmd)(nil), flags)
	MustRegisterCmd("gettpsview", (*GetTPSViewCmd)(nil), flags)
	MustRegisterCmd("gettpsreport", (*GetTPSViewCmd)(nil), flags)
	MustRegisterCmd("contractcall", (*ContractCallCmd)(nil), flags)
	MustRegisterCmd("tokenaddress", (*TokenAddressCmd)(nil), flags)
	MustRegisterCmd("trycontract", (*TryContractCmd)(nil), flags)
	MustRegisterCmd("getminerblock", (*GetMinerBlockCmd)(nil), flags)
	MustRegisterCmd("getmbk", (*GetMinerBlockCmd)(nil), flags)
	MustRegisterCmd("getblockchaininfo", (*GetBlockChainInfoCmd)(nil), flags)
	MustRegisterCmd("gbi", (*GetBlockChainInfoCmd)(nil), flags)
	MustRegisterCmd("addminingkey", (*AddMiningKeyCmd)(nil), flags)
	MustRegisterCmd("addcollateral", (*AddCollateralCmd)(nil), flags)
	MustRegisterCmd("getminerruntimemetrics", (*GetMinerRuntimeMetricsCmd)(nil), flags)
	MustRegisterCmd("getblockcount", (*GetBlockCountCmd)(nil), flags)
	MustRegisterCmd("gbc", (*GetBlockCountCmd)(nil), flags)
	MustRegisterCmd("getminerblockcount", (*GetMinerBlockCountCmd)(nil), flags)
	MustRegisterCmd("getblockhash", (*GetBlockHashCmd)(nil), flags)
	MustRegisterCmd("gbh", (*GetBlockHashCmd)(nil), flags)
	MustRegisterCmd("getminerblockhash", (*GetMinerBlockHashCmd)(nil), flags)
	MustRegisterCmd("getmbkh", (*GetMinerBlockHashCmd)(nil), flags)
	MustRegisterCmd("getminerblockheight", (*GetMinerBlockHeightCmd)(nil), flags)
	MustRegisterCmd("getblockheader", (*GetBlockHeaderCmd)(nil), flags)
	MustRegisterCmd("getblocktemplate", (*GetBlockTemplateCmd)(nil), flags)
	MustRegisterCmd("getcfilter", (*GetCFilterCmd)(nil), flags)
	MustRegisterCmd("getcfilterheader", (*GetCFilterHeaderCmd)(nil), flags)
	MustRegisterCmd("getchaintips", (*GetChainTipsCmd)(nil), flags)
	MustRegisterCmd("getconnectioncount", (*GetConnectionCountCmd)(nil), flags)
	MustRegisterCmd("resetconnection", (*ResetConnectionCmd)(nil), flags)
	MustRegisterCmd("getdifficulty", (*GetDifficultyCmd)(nil), flags)
	MustRegisterCmd("getgenerate", (*GetGenerateCmd)(nil), flags)
	MustRegisterCmd("gethashespersec", (*GetHashesPerSecCmd)(nil), flags)
	MustRegisterCmd("getinfo", (*GetInfoCmd)(nil), flags)
	MustRegisterCmd("getmempoolentry", (*GetMempoolEntryCmd)(nil), flags)
	MustRegisterCmd("getissuedtokens", (*GetIssuedTokensCmd)(nil), flags)

	MustRegisterCmd("getcrosschaindb", (*GetCrossChainDBCmd)(nil), flags)
	MustRegisterCmd("clearbtcl2pool", (*GetCrossChainDBCmd)(nil), flags)
	MustRegisterCmd("getxchtxfee", (*GetXChTxFeeCmd)(nil), flags)

	MustRegisterCmd("getmempoolinfo", (*GetMempoolInfoCmd)(nil), flags)
	MustRegisterCmd("createxferl2txo", (*Createxferl2txoCmd)(nil), flags)
	MustRegisterCmd("gettreasury", (*GetTreasuryCmd)(nil), flags)
	MustRegisterCmd("getsigners", (*GetSignersCmd)(nil), flags)
	MustRegisterCmd("getbtcpool", (*GetBtcPoolCmd)(nil), flags)
	MustRegisterCmd("getl2pool", (*GetL2PoolCmd)(nil), flags)
	MustRegisterCmd("getmininginfo", (*GetMiningInfoCmd)(nil), flags)
	MustRegisterCmd("getnetworkinfo", (*GetNetworkInfoCmd)(nil), flags)
	MustRegisterCmd("getnettotals", (*GetNetTotalsCmd)(nil), flags)
	MustRegisterCmd("getnetworkhashps", (*GetNetworkHashPSCmd)(nil), flags)
	MustRegisterCmd("getpeerinfo", (*GetPeerInfoCmd)(nil), flags)
	MustRegisterCmd("gps", (*GetPeerInfoCmd)(nil), flags)
	MustRegisterCmd("getrawmempool", (*GetRawMempoolCmd)(nil), flags)
	MustRegisterCmd("clearmempool", (*GetRawMempoolCmd)(nil), flags)
	MustRegisterCmd("getrawtransaction", (*GetRawTransactionCmd)(nil), flags)
	MustRegisterCmd("gettxout", (*GetTxOutCmd)(nil), flags)
	MustRegisterCmd("listutxos", (*ListUtxosCmd)(nil), flags)
	MustRegisterCmd("getdefine", (*GetDefineCmd)(nil), flags)
	MustRegisterCmd("getbtcl2Script", (*GetBtcL2ScriptCmd)(nil), flags)
	MustRegisterCmd("gettxoutproof", (*GetTxOutProofCmd)(nil), flags)
	MustRegisterCmd("gettxoutsetinfo", (*GetTxOutSetInfoCmd)(nil), flags)
	MustRegisterCmd("getwork", (*GetWorkCmd)(nil), flags)
	MustRegisterCmd("help", (*HelpCmd)(nil), flags)
	MustRegisterCmd("invalidateblock", (*InvalidateBlockCmd)(nil), flags)
	MustRegisterCmd("ping", (*PingCmd)(nil), flags)
	MustRegisterCmd("preciousblock", (*PreciousBlockCmd)(nil), flags)
	MustRegisterCmd("reconsiderblock", (*ReconsiderBlockCmd)(nil), flags)
	MustRegisterCmd("searchrawtransactions", (*SearchRawTransactionsCmd)(nil), flags)
	MustRegisterCmd("schrt", (*SearchRawTransactionsCmd)(nil), flags)
	MustRegisterCmd("searchspend", (*SearchRawSpendCmd)(nil), flags)
	MustRegisterCmd("sendrawtransaction", (*SendRawTransactionCmd)(nil), flags)
	MustRegisterCmd("srt", (*SendRawTransactionCmd)(nil), flags)
	MustRegisterCmd("confirmations", (*ConfirmationsCmd)(nil), flags)
	MustRegisterCmd("checkfork", (*CheckForkCmd)(nil), flags)
	MustRegisterCmd("recastrawtransaction", (*RecastRawTransactionCmd)(nil), flags)
	MustRegisterCmd("setgenerate", (*SetGenerateCmd)(nil), flags)
	MustRegisterCmd("stop", (*StopCmd)(nil), flags)
	MustRegisterCmd("submitblock", (*SubmitBlockCmd)(nil), flags)
	MustRegisterCmd("uptime", (*UptimeCmd)(nil), flags)
	MustRegisterCmd("validateaddress", (*ValidateAddressCmd)(nil), flags)
	MustRegisterCmd("verifymessage", (*VerifyMessageCmd)(nil), flags)
	MustRegisterCmd("verifytxoutproof", (*VerifyTxOutProofCmd)(nil), flags)
	MustRegisterCmd("verifysig", (*VerifySigCmd)(nil), flags)
	MustRegisterCmd("getchainmap", (*GetChainMapCmd)(nil), flags)

	MustRegisterCmd("dropminingkey", (*AddMiningKeyCmd)(nil), flags)
	MustRegisterCmd("dropcollateral", (*AddCollateralCmd)(nil), flags)
	MustRegisterCmd("listminingaddr", (*GetBlockChainInfoCmd)(nil), flags)
	MustRegisterCmd("listcollateral", (*GetBlockChainInfoCmd)(nil), flags)
	MustRegisterCmd("keyaddress", (*VerifySigCmd)(nil), flags)
}
