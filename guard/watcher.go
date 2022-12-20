package guard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tmlog "github.com/tendermint/tendermint/libs/log"

	"bitbucket.org/decimalteam/dsc-guard/fastclient"
)

type Watcher struct {
	node      string
	txData    []byte
	config    Config
	state     WatcherState
	guard     Guarder
	isRunning bool

	client *fastclient.FastClient
	logger tmlog.Logger

	lastValidatorHeight int64
	lastSignatureHeight int64
	muSetLastHeight     sync.Mutex
}

func NewWatcher(node string, config Config, guard Guarder, logger tmlog.Logger) *Watcher {
	return &Watcher{
		node:      node,
		config:    config,
		guard:     guard,
		isRunning: true,
		logger:    logger,
	}
}
func (w *Watcher) Start() {
	var err error
	var doBreak bool
	var counter = NewBlockCounter(5)
	w.state = WatcherConnecting
	var block int64

	// infinity loop: connect -> initial query (fallback to connect) -> watch block events (fallback to connect)
	for w.isRunning {
		switch w.state {

		case WatcherConnecting:
			for w.isRunning {
				w.CleanUp()
				w.guard.ReportWatcher(w.node, WatcherConnecting)
				// 1. create client
				w.client = fastclient.NewFastClient(w.node, time.Duration(w.config.NewBlockTimeout)*time.Second)
				err := w.client.CheckConnection()
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] Error in connecting: %s", w.node, err.Error()))
					time.Sleep(time.Second * time.Duration(w.config.FallbackPause))
					continue
				}

				// 2. all ok, change state
				w.state = WatcherQueryValidator
				break
			}

		case WatcherQueryValidator:
			{
				w.guard.ReportWatcher(w.node, WatcherQueryValidator)
				// query initial information from node: last height, validator set
				_, err = w.queryValidatorSet()
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] queryValidatorSet: %s", w.node, err.Error()))
					w.state = WatcherConnecting
				} else {
					w.state = WatcherWatching
				}
				break
			}

		case WatcherWatching:
			w.guard.ReportWatcher(w.node, WatcherWatching)
			for w.isRunning {
				doBreak = false
				err := w.querySignatures()
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] querySignatures error: %s", w.node, err.Error()))
					doBreak = true
				}
				block, err = w.queryValidatorSet()
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] queryValidatorSet error: %s", w.node, err.Error()))
					doBreak = true
				}
				if counter.increment(block) {
					w.checkTxData()
				}
				// if timout or error occurs
				if doBreak {
					w.state = WatcherConnecting
					break
				}
				// Decimal: >5 sec per block
				time.Sleep(time.Second * 5)
			}
		}
	}
}

func (w *Watcher) Stop() {
	w.isRunning = false
}

func (w *Watcher) CleanUp() {
	w.lastValidatorHeight = 0
	w.lastSignatureHeight = 0
}

func (w *Watcher) SetTxData(txData []byte) {
	w.txData = txData
}

func (w *Watcher) checkTxData() {
	res, err := w.client.CheckTx(w.txData)
	if err != nil {
		w.logger.Error(fmt.Sprintf("[%s] CheckTx error: %s", w.node, err.Error()))
		return
	}
	if res.Code != 0 {
		w.logger.Error(fmt.Sprintf("[%s] Check set_offline transaction: code=%d, codespace=%s, log=%s", w.node, res.Code, res.Codespace, res.Log))
		w.guard.ReportTxValidity(w.node, false)
		return
	}
	w.logger.Info(fmt.Sprintf("[%s] Check set_offline transaction ok", w.node))
	w.guard.ReportTxValidity(w.node, true)
}

func (w *Watcher) SendOffline() {
	if w.txData == nil {
		w.logger.Error(fmt.Sprintf("[%s] set_offline transaction is null", w.node))
		return
	}
	if w.state != WatcherWatching {
		w.logger.Error(fmt.Sprintf("[%s] Watcher not watching", w.node))
		return
	}
	res, err := w.client.BroadcastTxSync(w.txData)
	if err != nil {
		w.logger.Error(fmt.Sprintf("[%s] BroadcastTxSync error: %s", w.node, err.Error()))
		return
	}
	if res.Code != 0 {
		w.logger.Error(fmt.Sprintf("[%s] BroadcastTxSync set_offline transaction: code=%d, codespace=%s, log=%s", w.node, res.Code, res.Codespace, res.Log))
	}
	w.txData = nil
	w.logger.Info(fmt.Sprintf("[%s] BroadcastTxSync succesful", w.node))
}

// return true if block isn't outdated
func (w *Watcher) SetLastValidatorHeight(height int64) bool {
	if height <= w.lastValidatorHeight {
		return false
	}
	w.lastValidatorHeight = height
	return true
}

func (w *Watcher) SetLastSignatureHeight(height int64) bool {
	if height <= w.lastSignatureHeight {
		return false
	}
	w.lastSignatureHeight = height
	return true
}

func (w *Watcher) queryValidatorSet() (int64, error) {
	// Retrieve set of validators expected in the block
	validatorSet, err := w.client.Validators()
	if err != nil {
		return 0, fmt.Errorf("call Validators(): %s", err.Error())
	}
	isNew := w.SetLastValidatorHeight(validatorSet.BlockHeight)
	w.logger.Info(fmt.Sprintf("[%s] Retrieved set of validators for block %d", w.node, w.lastValidatorHeight))

	if isNew {
		// Check if validato in set and has power
		for _, v := range validatorSet.Validators {
			if strings.EqualFold(v.Address, w.config.ValidatorAddress) {
				w.logger.Info(fmt.Sprintf("[%s] validator in set: %s", w.node, v.Address))
				w.guard.ReportValidatorOnline(w.node, w.lastValidatorHeight, v.VotingPower > "0")
				return w.lastValidatorHeight, nil
			}
		}
		// validator not found
		w.guard.ReportValidatorOnline(w.node, w.lastValidatorHeight, false)
		w.logger.Info(fmt.Sprintf("[%s] validator not in set: %s", w.node, w.config.ValidatorAddress))
	}

	return w.lastValidatorHeight, nil
}

func (w *Watcher) querySignatures() error {
	// Retrieve signatures of last block
	signatures, err := w.client.BlockSignatures()
	if err != nil {
		return fmt.Errorf("call Validators(): %s", err.Error())
	}
	isNew := w.SetLastSignatureHeight(signatures.Height)
	w.logger.Info(fmt.Sprintf("[%s] Retrieved signatures for block %d", w.node, w.lastSignatureHeight))

	if isNew {
		// Check if it is expected that block is signed by guarded validator's node
		signed := false
		for _, sgn := range signatures.Signatures {
			if strings.EqualFold(sgn.Address, w.config.ValidatorAddress) && sgn.Signature > "" {
				signed = len(sgn.Signature) > 0
				break
			}
		}
		w.guard.SetSign(w.lastSignatureHeight, signed)
	}

	return nil
}

type blockCounter struct {
	lastBlock int64
	counter   int64
	limit     int64
	mu        sync.Mutex
}

func NewBlockCounter(limit int64) *blockCounter {
	return &blockCounter{
		lastBlock: 0,
		counter:   0,
		limit:     limit,
	}
}

func (bc *blockCounter) increment(block int64) bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if block > bc.lastBlock {
		bc.lastBlock = block
		bc.counter++
	}

	if bc.counter >= bc.limit {
		bc.counter = 0
	}
	// check on start
	if bc.counter == 1 {
		return true
	}
	return false
}
