package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type RPCClient struct {
	url        string
	user       string
	pass       string
	httpClient *http.Client
	nextID     int64
}

func NewRPCClient(url, user, pass string, timeout time.Duration) *RPCClient {
	return &RPCClient{
		url:  url,
		user: user,
		pass: pass,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *RPCClient) GetBlockCount(ctx context.Context) (int64, error) {
	raw, err := c.call(ctx, "getblockcount")
	if err != nil {
		return 0, err
	}
	return decodeRPCInt64(raw)
}

func (c *RPCClient) GetBlockHash(ctx context.Context, height int64) (string, error) {
	raw, err := c.call(ctx, "getblockhash", height)
	if err != nil {
		return "", err
	}
	return decodeRPCString(raw)
}

func (c *RPCClient) GetBlock(ctx context.Context, hash string) (*TxBlock, error) {
	raw, err := c.call(ctx, "getblock", hash, true, true)
	if err != nil {
		return nil, err
	}
	var block TxBlock
	if err := decodeRaw(raw, &block); err != nil {
		return nil, err
	}
	return &block, nil
}

func (c *RPCClient) GetMinerBlockCount(ctx context.Context) (int64, error) {
	raw, err := c.call(ctx, "getminerblockcount")
	if err != nil {
		return 0, err
	}
	return decodeRPCInt64(raw)
}

func (c *RPCClient) GetMinerBlockHash(ctx context.Context, height int64) (string, error) {
	raw, err := c.call(ctx, "getminerblockhash", height)
	if err != nil {
		return "", err
	}
	return decodeRPCString(raw)
}

func (c *RPCClient) GetMinerBlock(ctx context.Context, hash string) (*MinerBlock, error) {
	raw, err := c.call(ctx, "getminerblock", hash, true)
	if err != nil {
		return nil, err
	}
	var block MinerBlock
	if err := decodeRaw(raw, &block); err != nil {
		return nil, err
	}
	return &block, nil
}

func (c *RPCClient) call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
	c.nextID++
	requestBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      c.nextID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s RPC request failed: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s RPC response read failed: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s RPC HTTP status %s", method, resp.Status)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ID interface{} `json:"id"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("%s RPC JSON decode failed: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("%s RPC error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func decodeRaw(raw json.RawMessage, dst interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

func decodeRPCString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeRPCInt64(raw json.RawMessage) (int64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strconv.ParseInt(s, 10, 64)
	}

	var number json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&number); err != nil {
		return 0, err
	}
	return number.Int64()
}
