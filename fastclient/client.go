package fastclient

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// FastClient need to get block fast without block events
type FastClient struct {
	basePath string
	conn     http.Client
}

type TmValidator struct {
	Address     string `json:"address"`
	VotingPower string `json:"voting_power"`
}

type ValidatorSet struct {
	BlockHeight int64         `json:"block_height"`
	Validators  []TmValidator `json:"validators"`
}

type BlockSignatures struct {
	Height     int64
	Signatures []ValidatorSignature
}

type ValidatorSignature struct {
	Address   string `json:"validator_address"`
	Signature string `json:"signature"`
}

type RpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (e RpcError) Error() string {
	return e.Data
}

func NewFastClient(basePath string, timeout time.Duration) *FastClient {
	var fc FastClient
	limitedTransport := http.Transport{
		MaxIdleConns:    2,
		IdleConnTimeout: time.Minute,
	}
	fc.basePath = basePath
	fc.conn = http.Client{
		Transport: &limitedTransport,
		Timeout:   timeout,
	}
	return &fc
}

func (fc *FastClient) CheckConnection() error {
	resp, err := fc.conn.Get(fc.basePath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http code %d", resp.StatusCode)
	}
	return nil
}

func (fc *FastClient) Validators() (ValidatorSet, error) {
	type rpcResult struct {
		Error  RpcError `json:"error"`
		Result struct {
			BlockHeight string        `json:"block_height"`
			Validators  []TmValidator `json:"validators"`
		} `json:"result"`
	}
	// Decimal validators limit is 160
	resp, err := fc.conn.Get(fc.basePath + "/validators?per_page=200")
	if err != nil {
		return ValidatorSet{}, err
	}
	defer resp.Body.Close()
	var result rpcResult
	bz, err := io.ReadAll(resp.Body)
	if err != nil {
		return ValidatorSet{}, err
	}
	err = json.Unmarshal(bz, &result)
	if err != nil {
		return ValidatorSet{}, err
	}
	if result.Error.Code != 0 {
		return ValidatorSet{}, result.Error
	}

	var vs ValidatorSet
	vs.BlockHeight, _ = strconv.ParseInt(result.Result.BlockHeight, 10, 64)
	vs.Validators = append(vs.Validators, result.Result.Validators...)
	return vs, nil
}

func (fc *FastClient) BlockSignatures() (BlockSignatures, error) {
	type rpcResult struct {
		Result struct {
			Block struct {
				Header struct {
					Height string `json:"height"`
				} `json:"header"`
				LastCommit struct {
					Height     string               `json:"height"`
					Signatures []ValidatorSignature `json:"signatures"`
				} `json:"last_commit"`
			} `json:"block"`
		} `json:"result"`
	}
	resp, err := fc.conn.Get(fc.basePath + "/block")
	if err != nil {
		return BlockSignatures{}, err
	}
	defer resp.Body.Close()
	var result rpcResult
	bz, err := io.ReadAll(resp.Body)
	if err != nil {
		return BlockSignatures{}, err
	}
	err = json.Unmarshal(bz, &result)
	if err != nil {
		return BlockSignatures{}, err
	}

	var bs BlockSignatures
	bs.Height, _ = strconv.ParseInt(result.Result.Block.Header.Height, 10, 64) // there must be int
	// Fix to sync with Validators
	// bs.Height += 1
	bs.Signatures = append(bs.Signatures, result.Result.Block.LastCommit.Signatures...)
	return bs, nil
}

type CheckTxResult struct {
	Code      int    `json:"code"`
	Codespace string `json:"codespace"`
	Log       string `json:"log"`
}

func (fc *FastClient) CheckTx(tx []byte) (CheckTxResult, error) {
	type rpcResult struct {
		Result CheckTxResult `json:"result"`
	}
	resp, err := fc.conn.Get(fc.basePath + "/check_tx?tx=0x" + hex.EncodeToString(tx))
	if err != nil {
		return CheckTxResult{}, err
	}
	defer resp.Body.Close()
	var result rpcResult
	bz, err := io.ReadAll(resp.Body)
	if err != nil {
		return CheckTxResult{}, err
	}
	err = json.Unmarshal(bz, &result)
	if err != nil {
		return CheckTxResult{}, err
	}
	return result.Result, nil
}

func (fc *FastClient) BroadcastTxSync(tx []byte) (CheckTxResult, error) {
	type rpcResult struct {
		Result CheckTxResult `json:"result"`
	}
	resp, err := fc.conn.Get(fc.basePath + "/broadcast_tx_sync?tx=0x" + hex.EncodeToString(tx))
	if err != nil {
		return CheckTxResult{}, err
	}
	var result rpcResult
	bz, err := io.ReadAll(resp.Body)
	if err != nil {
		return CheckTxResult{}, err
	}
	err = json.Unmarshal(bz, &result)
	if err != nil {
		return CheckTxResult{}, err
	}
	return result.Result, nil
}
