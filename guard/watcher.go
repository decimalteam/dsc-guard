package guard

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tmlog "github.com/tendermint/tendermint/libs/log"
	tmclient "github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
	tmtypes "github.com/tendermint/tendermint/types"
)

type Watcher struct {
	node      string
	txData    []byte
	config    Config
	state     WatcherState
	guard     Guarder
	isRunning bool

	client *tmclient.HTTP
	logger tmlog.Logger

	blockEvents     <-chan coretypes.ResultEvent
	validatorEvents <-chan coretypes.ResultEvent
	lastHeight      int64
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
	var ctx = context.Background()
	// infinity loop: connect -> initial query (fallback to connect) -> watch block events (fallback to connect)
	for w.isRunning {
		switch w.state {

		case WatcherConnecting:
			for w.isRunning {
				w.CleanUp()
				w.guard.ReportWatcher(w.node, WatcherConnecting)
				// 1. create client
				w.client, err = tmclient.New(w.node, "/websocket")
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] Error in connecting: %s", w.node, err.Error()))
					time.Sleep(time.Second * time.Duration(w.config.FallbackPause))
					continue
				}

				err = w.client.Start()
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] Error in start watching: %s", w.node, err.Error()))
					time.Sleep(time.Second * time.Duration(w.config.FallbackPause))
					continue
				}

				// 2. subscribe
				// tmtypes.QueryForEvent(tmtypes.EventNewBlockValue).String()
				w.blockEvents, err = w.client.Subscribe(ctx, Subscriber, QueryNewBlock, ChannelCapacity)
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] Error in subscribe (%s): %s", w.node, QueryNewBlock, err.Error()))
					time.Sleep(time.Second * time.Duration(w.config.FallbackPause))
					continue
				}

				w.validatorEvents, err = w.client.Subscribe(ctx, Subscriber, QueryValidatorSet, ChannelCapacity)
				if err != nil {
					w.logger.Error(fmt.Sprintf("[%s] Error in subscribe (%s): %s", w.node, QueryValidatorSet, err.Error()))
					time.Sleep(time.Second * time.Duration(w.config.FallbackPause))
					continue
				}

				// 3. all ok, change state
				w.state = WatcherQueryValidator
				break
			}

		case WatcherQueryValidator:
			{
				w.guard.ReportWatcher(w.node, WatcherQueryValidator)
				// query initial information from node: last height, validator set
				err = w.queryValidatorSet()
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
				select {
				case result := <-w.blockEvents:
					block, err := w.handleEventNewBlock(result)
					if err != nil {
						w.logger.Error(fmt.Sprintf("[%s] NewBlock processing error: %s", w.node, err.Error()))
						doBreak = true
					}
					if counter.increment(block) {
						w.CheckTxData()
					}
				case result := <-w.validatorEvents:
					err = w.handleEventValidatorSetUpdates(result)
					if err != nil {
						w.logger.Error(fmt.Sprintf("[%s] ValidatorSetUpdates processing error: %s", w.node, err.Error()))
						doBreak = true
					}
				case <-time.After(time.Duration(w.config.NewBlockTimeout) * time.Second):
					{
						w.logger.Error(fmt.Sprintf("[%s] New block timeout reached", w.node))
						doBreak = true
					}
				case <-w.client.Quit():
					{
						w.logger.Debug(fmt.Sprintf("[%s] Tendermint client quit", w.node))
						doBreak = true
					}
				}
				// if timout or error occurs
				if doBreak {
					w.state = WatcherConnecting
					break
				}
			}
		}
	}
}

func (w *Watcher) Stop() {
	w.isRunning = false
}

func (w *Watcher) CleanUp() {
	// Unsubscribe
	if w.client != nil && w.client.IsRunning() {
		err := w.client.UnsubscribeAll(context.Background(), Subscriber)
		if err != nil {
			w.logger.Error(fmt.Sprintf("[%s] UnsubscribeAll error: %s", w.node, err.Error()))
		}
	}
	// stop tendermint client
	if w.client != nil {
		err := w.client.Stop()
		if err != nil {
			w.logger.Error(fmt.Sprintf("[%s] Stop error: %s", w.node, err.Error()))
		}
	}
	w.blockEvents = nil
	w.validatorEvents = nil
	w.lastHeight = 0
}

func (w *Watcher) SetTxData(txData []byte) {
	w.txData = txData
}

func (w *Watcher) CheckTxData() {
	res, err := w.client.CheckTx(context.Background(), w.txData)
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
	res, err := w.client.BroadcastTxSync(context.Background(), w.txData)
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

// //////////////////////////////////////////////////////////////////////////////
// Handling events from Tendermint client
// //////////////////////////////////////////////////////////////////////////////

func (w *Watcher) handleEventNewBlock(result coretypes.ResultEvent) (block int64, err error) {
	event, ok := result.Data.(tmtypes.EventDataNewBlock)
	if !ok {
		return 0, fmt.Errorf("unable to cast received event to struct tmtypes.EventDataNewBlock: %T", result.Data)
	}

	w.logger.Info(fmt.Sprintf("[%s] Received new block %d", w.node, event.Block.Height))

	w.lastHeight = event.Block.Height
	signed := false
	for _, s := range event.Block.LastCommit.Signatures {
		if strings.EqualFold(s.ValidatorAddress.String(), w.config.ValidatorAddress) {
			signed = len(s.Signature) > 0
			break
		}
	}

	w.guard.SetSign(w.lastHeight, signed)

	return event.Block.Height, nil
}

func (w *Watcher) handleEventValidatorSetUpdates(result coretypes.ResultEvent) (err error) {
	event, ok := result.Data.(tmtypes.EventDataValidatorSetUpdates)
	if !ok {
		return fmt.Errorf("unable to cast received event to struct tmtypes.EventDataValidatorSetUpdates")
	}

	w.logger.Info(fmt.Sprintf("[%s] Received new validator set updates", w.node))

	for _, validator := range event.ValidatorUpdates {
		if strings.EqualFold(validator.Address.String(), w.config.ValidatorAddress) {
			w.guard.ReportValidatorOnline(w.node, w.lastHeight, validator.VotingPower > 0)
			return nil
		}
	}
	// validator not found
	w.guard.ReportValidatorOnline(w.node, w.lastHeight, false)

	return nil
}

func (w *Watcher) queryValidatorSet() error {
	status, err := w.client.Status(context.Background())
	if err != nil {
		return fmt.Errorf("call Status(): %s", err.Error())
	}
	w.lastHeight = status.SyncInfo.LatestBlockHeight

	w.logger.Info(fmt.Sprintf("[%s] Retrieving set of validators for block %d", w.node, w.lastHeight))

	var page int = 1
	var perPage int = 1000

	// Retrieve set of validators expected in the block
	validators, err := w.client.Validators(context.Background(), &w.lastHeight, &page, &perPage)
	if err != nil {
		return fmt.Errorf("call Validators(): %s", err.Error())
	}

	// Check if it is expected that block is signed by guarded validator's node
	for _, v := range validators.Validators {
		w.logger.Info(fmt.Sprintf("[%s] validator in set: %s", w.node, v.Address.String()))
		if strings.EqualFold(v.Address.String(), w.config.ValidatorAddress) {
			w.guard.ReportValidatorOnline(w.node, w.lastHeight, v.VotingPower > 0)
			return nil
		}
	}
	// validator not found
	w.guard.ReportValidatorOnline(w.node, w.lastHeight, false)

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
		return true
	}
	return false
}
