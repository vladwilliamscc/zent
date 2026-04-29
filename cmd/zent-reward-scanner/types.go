package main

import (
	"context"
	"encoding/json"
)

type ChainClient interface {
	GetBlockCount(ctx context.Context) (int64, error)
	GetBlockHash(ctx context.Context, height int64) (string, error)
	GetBlock(ctx context.Context, hash string) (*TxBlock, error)
	GetMinerBlockCount(ctx context.Context) (int64, error)
	GetMinerBlockHash(ctx context.Context, height int64) (string, error)
	GetMinerBlock(ctx context.Context, hash string) (*MinerBlock, error)
}

type ScanConfig struct {
	StartHeight      int64
	EndHeight        int64
	StartMinerHeight int64
	EndMinerHeight   int64
	Workers          map[string]string
}

type TxBlock struct {
	Hash   string  `json:"hash"`
	Height int64   `json:"height"`
	RawTx  []TxRaw `json:"rawtx"`
}

type TxRaw struct {
	TxID string `json:"txid"`
	Vout []Vout `json:"vout"`
}

type Vout struct {
	TokenType    uint64          `json:"tokentype"`
	Value        json.RawMessage `json:"value"`
	N            uint32          `json:"n"`
	ScriptPubKey ScriptPubKey    `json:"scriptPubKey"`
}

type ScriptPubKey struct {
	Addresses []string `json:"addresses"`
	Type      string   `json:"type"`
	Asm       string   `json:"asm"`
	Hex       string   `json:"hex"`
}

type MinerBlock struct {
	Hash       string `json:"hash"`
	Height     int64  `json:"height"`
	Address    string `json:"address"`
	Best       string `json:"best"`
	Connection string `json:"connection"`
	Collateral string `json:"collateral"`
}

type Report struct {
	Metadata                ScanMetadata      `json:"metadata"`
	Workers                 []WorkerReport    `json:"workers"`
	TotalRewardAtoms        int64             `json:"total_reward_atoms"`
	TotalRewardDecimal      string            `json:"total_reward_decimal"`
	AttributedRewardAtoms   int64             `json:"attributed_reward_atoms"`
	AttributedRewardDecimal string            `json:"attributed_reward_decimal"`
	UnmatchedRewardAtoms    int64             `json:"unmatched_reward_atoms"`
	UnmatchedRewardDecimal  string            `json:"unmatched_reward_decimal"`
	UnmatchedRewards        []UnmatchedReward `json:"unmatched_rewards"`
}

type ScanMetadata struct {
	TxStartHeight      int64    `json:"tx_start_height"`
	TxEndHeight        int64    `json:"tx_end_height"`
	MinerStartHeight   int64    `json:"miner_start_height"`
	MinerEndHeight     int64    `json:"miner_end_height"`
	TxBlocksScanned    int64    `json:"tx_blocks_scanned"`
	MinerBlocksScanned int64    `json:"miner_blocks_scanned"`
	Warnings           []string `json:"warnings,omitempty"`
}

type WorkerReport struct {
	Worker            string        `json:"worker"`
	MiningAddress     string        `json:"mining_address"`
	AcceptedMRBlocks  int64         `json:"accepted_mrblocks"`
	RewardAtoms       int64         `json:"reward_atoms"`
	RewardDecimal     string        `json:"reward_decimal"`
	FirstHeight       *int64        `json:"first_height,omitempty"`
	LastHeight        *int64        `json:"last_height,omitempty"`
	AcceptedMRHeights []int64       `json:"accepted_mr_heights,omitempty"`
	RewardEvents      []RewardEvent `json:"reward_events,omitempty"`
}

type RewardEvent struct {
	Height        int64  `json:"height"`
	BlockHash     string `json:"block_hash"`
	TxID          string `json:"txid"`
	Vout          uint32 `json:"vout"`
	MiningAddress string `json:"mining_address"`
	Worker        string `json:"worker"`
	RewardAtoms   int64  `json:"reward_atoms"`
	RewardDecimal string `json:"reward_decimal"`
	Reason        string `json:"reason"`
}

type UnmatchedReward struct {
	Height        int64    `json:"height"`
	BlockHash     string   `json:"block_hash"`
	TxID          string   `json:"txid"`
	Vout          uint32   `json:"vout"`
	Addresses     []string `json:"addresses,omitempty"`
	RewardAtoms   int64    `json:"reward_atoms"`
	RewardDecimal string   `json:"reward_decimal"`
	Reason        string   `json:"reason"`
}
