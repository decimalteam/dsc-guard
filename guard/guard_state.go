package guard

import (
	"fmt"
	"sync"
	"time"

	tmlog "github.com/tendermint/tendermint/libs/log"
)

// State machine contains business logic to

type GuardStateMachine struct {
	signWindow        []bool
	currentHeight     int64
	config            Config
	state             GlobalState
	eventChannel      chan interface{}
	eventReadTimeout  time.Duration
	watchersState     map[string]WatcherState
	isTxValid         map[string]bool
	isValidatorOnline bool
	isSkipSign        bool

	logger tmlog.Logger

	isRunning bool // flag for Start/Stop

	setOfflineCallback setOfflineFunc

	mu sync.Mutex
}

// minimal interface for Watcher
type Guarder interface {
	ReportWatcher(id string, state WatcherState)
	ReportTxValidity(id string, valid bool)
	ReportValidatorOnline(id string, height int64, online bool)
	SetSign(height int64, signed bool)
}

type setOfflineFunc func()

func NewGuardState(logger tmlog.Logger, config Config, callback setOfflineFunc) *GuardStateMachine {
	var signWindow = make([]bool, config.MissedBlocksWindow)
	sm := &GuardStateMachine{
		eventChannel:       make(chan interface{}, 1000),
		watchersState:      make(map[string]WatcherState),
		isTxValid:          make(map[string]bool),
		isValidatorOnline:  false,
		state:              StateStarting,
		logger:             logger,
		config:             config,
		signWindow:         signWindow,
		setOfflineCallback: callback,
		isRunning:          false,
	}
	sm.ResetWindow()
	return sm
}

func (sm *GuardStateMachine) ProcessEvent(ev interface{}) {
	txValid, ok := ev.(eventTxValidity)
	if ok {
		sm.isTxValid[txValid.node] = txValid.valid
	}
	// TODO: check correctness of summaryValidatorOnline for multiple watchers
	// when watchers online-offline, skip blocks etc.
	valState, ok := ev.(eventValidatorState)
	if ok {
		if valState.height >= sm.currentHeight {
			sm.isValidatorOnline = valState.online
		}
	}
	watcherState, ok := ev.(eventWatcherState)
	if ok {
		sm.watchersState[watcherState.node] = watcherState.state
	}
	_, ok = ev.(eventValidatorSkipSign)
	if ok {
		sm.isSkipSign = true
	}
	// process event, change state
	switch sm.state {
	case StateStarting:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.state = StateConnecting
				sm.logger.Debug("guard state transition StateStarting->StateConnecting")
				break
			}
			if sm.summaryWatcherState() == WatcherWatching {
				sm.ResetWindow()
				sm.isSkipSign = false
				sm.state = StateWatching
				if !sm.summaryValidatorOnline() && sm.summaryTxValid() {
					sm.logger.Debug("guard state transition StateStarting->StateValidatorIsOffline")
					sm.state = StateValidatorIsOffline
				}
				if !sm.summaryTxValid() {
					sm.logger.Debug("guard state transition StateStarting->StateWatchingWithoutTx")
					sm.state = StateWatchingWithoutTx
				}
				break
			}
		}
	case StateConnecting:
		{
			if sm.summaryWatcherState() == WatcherWatching && sm.summaryValidatorOnline() && sm.summaryTxValid() {
				sm.logger.Debug("guard state transition StateConnecting->StateWatching")
				sm.state = StateWatching
				break
			}
			if sm.summaryWatcherState() == WatcherWatching && !sm.summaryValidatorOnline() && sm.summaryTxValid() {
				sm.logger.Debug("guard state transition StateConnecting->StateValidatorIsOffline")
				sm.state = StateValidatorIsOffline
				break
			}
			if sm.summaryWatcherState() == WatcherWatching && !sm.summaryTxValid() {
				sm.logger.Debug("guard state transition StateConnecting->StateWatchingWithoutTx")
				sm.state = StateWatchingWithoutTx
				break
			}
		}
	case StateWatching:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.logger.Debug("guard state transition StateWatching->StateConnecting")
				sm.state = StateConnecting
				break
			}
			if !sm.summaryTxValid() {
				sm.logger.Debug("guard state transition StateWatching->StateWatchingWithoutTx")
				sm.state = StateWatchingWithoutTx
				break
			}
			if sm.summaryTxValid() && !sm.summaryValidatorOnline() {
				sm.logger.Debug("guard state transition StateWatching->StateValidatorIsOffline")
				sm.state = StateValidatorIsOffline
				break
			}
			if sm.isSkipSign {
				sm.logger.Info("guard: send set_offline")
				sm.setOfflineCallback()
				sm.logger.Debug("guard state transition StateWatching->StateStarting")
				sm.state = StateStarting
				break
			}
		}
	case StateValidatorIsOffline:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.logger.Debug("guard state transition StateValidatorIsOffline->StateConnecting")
				sm.state = StateConnecting
				break
			}
			if sm.summaryTxValid() && sm.summaryValidatorOnline() {
				sm.logger.Debug("guard state transition StateValidatorIsOffline->StateWatching")
				sm.state = StateWatching
				break
			}
		}
	case StateWatchingWithoutTx:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.logger.Debug("guard state transition StateWatchingWithoutTx->StateConnecting")
				sm.state = StateConnecting
				break
			}
			if sm.summaryTxValid() && sm.summaryValidatorOnline() {
				sm.logger.Debug("guard state transition StateWatchingWithoutTx->StateWatching")
				sm.state = StateWatching
				break
			}
			if sm.summaryTxValid() && !sm.summaryValidatorOnline() {
				sm.logger.Debug("guard state transition StateWatchingWithoutTx->StateValidatorIsOffline")
				sm.state = StateValidatorIsOffline
				break
			}
			if sm.summaryValidatorOnline() {
				sm.logger.Error("Validator is online, but transaction is invalid! You can't protect validator from slash")
			}
		}
	}
}

func (sm *GuardStateMachine) Start() {
	sm.isRunning = true
	sm.state = StateStarting
	for sm.isRunning {
		select {
		case ev := <-sm.eventChannel:
			{
				sm.ProcessEvent(ev)
			}
		case <-time.After(sm.eventReadTimeout):
			{
				continue
			}
		}

	}
}

func (sm *GuardStateMachine) Stop() {
	sm.isRunning = false
}

func (sm *GuardStateMachine) ResetWindow() {
	for i := range sm.signWindow {
		sm.signWindow[i] = true
	}
}

func (sm *GuardStateMachine) SetSign(height int64, signed bool) {
	if !sm.isRunning {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if height <= sm.currentHeight {
		return
	}
	if sm.currentHeight == 0 {
		sm.currentHeight = height
	} else {
		// TODO: do we overslept something? invalid state?
		// if height-sm.currentHeight > 1 {
		// }
	}
	sm.currentHeight = height
	if sm.summaryValidatorOnline() {
		sm.signWindow[int(sm.currentHeight)%sm.config.MissedBlocksWindow] = signed
	} else {
		sm.signWindow[int(sm.currentHeight)%sm.config.MissedBlocksWindow] = true
	}
	notSignedCount := 0
	for _, signed := range sm.signWindow {
		if !signed {
			notSignedCount++
		}
	}

	sm.logger.Debug(fmt.Sprintf("missed blocks in window = %d", notSignedCount))

	if notSignedCount >= sm.config.MissedBlocksLimit {
		sm.eventChannel <- eventValidatorSkipSign{}
	}
}

func (sm *GuardStateMachine) ReportWatcher(id string, state WatcherState) {
	if !sm.isRunning {
		return
	}
	sm.eventChannel <- eventWatcherState{node: id, state: state}
}

func (sm *GuardStateMachine) ReportTxValidity(id string, valid bool) {
	if !sm.isRunning {
		return
	}
	sm.eventChannel <- eventTxValidity{node: id, valid: valid}
}

func (sm *GuardStateMachine) ReportValidatorOnline(id string, height int64, online bool) {
	if !sm.isRunning {
		return
	}
	if height < sm.currentHeight {
		return
	}
	sm.eventChannel <- eventValidatorState{node: id, height: height, online: online}
}

func (sm *GuardStateMachine) summaryWatcherState() WatcherState {
	for _, ws := range sm.watchersState {
		if ws == WatcherWatching {
			return WatcherWatching
		}
	}
	return WatcherConnecting
}

func (sm *GuardStateMachine) summaryTxValid() bool {
	if len(sm.isTxValid) == 0 {
		return false
	}
	for _, b := range sm.isTxValid {
		if !b {
			return false
		}
	}
	return true
}

func (sm *GuardStateMachine) summaryValidatorOnline() bool {
	return sm.isValidatorOnline
}
