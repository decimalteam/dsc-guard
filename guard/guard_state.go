package guard

import (
	"sync"
	"time"
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
	isValidatorOnline map[string]bool
	isSkipSign        bool

	isRunning bool // flag for Start/Stop

	setOfflineCallback setOfflineFunc

	mu sync.Mutex
}

// minimal interface for Watcher
type Guarder interface {
	ReportWatcher(id string, state WatcherState)
	ReportTxValidity(id string, valid bool)
	ReportValidatorOnline(height int64, online bool)
	SetSign(height int64, signed bool)
}

type setOfflineFunc func()

func NewGuardState(config Config, callback setOfflineFunc) *GuardStateMachine {
	var signWindow = make([]bool, config.MissedBlocksWindow)
	sm := &GuardStateMachine{
		config:     config,
		signWindow: signWindow,
	}
	sm.eventChannel = make(chan interface{}, 1)
	sm.watchersState = make(map[string]WatcherState)
	sm.isTxValid = make(map[string]bool)
	sm.isValidatorOnline = make(map[string]bool)
	sm.state = StateStarting
	sm.setOfflineCallback = callback
	sm.isRunning = true
	sm.ResetWindow()
	return sm
}

func (sm *GuardStateMachine) ProcessEvent(ev interface{}) {
	txValid, ok := ev.(eventTxValidity)
	if ok {
		sm.isTxValid[txValid.node] = txValid.valid
	}
	valState, ok := ev.(eventValidatorState)
	if ok {
		sm.isValidatorOnline[valState.node] = valState.online
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
				break
			}
			if sm.summaryWatcherState() == WatcherWatching {
				sm.ResetWindow()
				sm.isSkipSign = false
				sm.state = StateWatching
				if !sm.summaryValidatorOnline() && sm.summaryTxValid() {
					sm.state = StateValidatorIsOffline
				}
				if !sm.summaryTxValid() {
					sm.state = StateWatchingWithoutTx
				}
				break
			}
		}
	case StateConnecting:
		{
			if sm.summaryWatcherState() == WatcherWatching && sm.summaryValidatorOnline() && sm.summaryTxValid() {
				sm.state = StateWatching
				break
			}
			if sm.summaryWatcherState() == WatcherWatching && !sm.summaryValidatorOnline() && sm.summaryTxValid() {
				sm.state = StateValidatorIsOffline
				break
			}
			if sm.summaryWatcherState() == WatcherWatching && !sm.summaryTxValid() {
				sm.state = StateWatchingWithoutTx
				break
			}
		}
	case StateWatching:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.state = StateConnecting
				break
			}
			if !sm.summaryTxValid() {
				sm.state = StateWatchingWithoutTx
				break
			}
			if sm.summaryTxValid() && !sm.summaryValidatorOnline() {
				sm.state = StateValidatorIsOffline
				break
			}
			if sm.isSkipSign {
				sm.setOfflineCallback()
				sm.state = StateStarting
				break
			}
		}
	case StateValidatorIsOffline:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.state = StateConnecting
				break
			}
			if sm.summaryTxValid() && sm.summaryValidatorOnline() {
				sm.state = StateWatching
				break
			}
		}
	case StateWatchingWithoutTx:
		{
			if sm.summaryWatcherState() == WatcherConnecting {
				sm.state = StateConnecting
				break
			}
			if sm.summaryTxValid() && sm.summaryValidatorOnline() {
				sm.state = StateWatching
				break
			}
			if sm.summaryTxValid() && !sm.summaryValidatorOnline() {
				sm.state = StateValidatorIsOffline
				break
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
	sm.signWindow[int(sm.currentHeight)%sm.config.MissedBlocksWindow] = signed
	notSignedCount := 0
	for _, signed := range sm.signWindow {
		if !signed {
			notSignedCount++
		}
	}
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

func (sm *GuardStateMachine) ReportValidatorOnline(id string, online bool) {
	if !sm.isRunning {
		return
	}
	sm.eventChannel <- eventValidatorState{node: id, online: online}
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
	if len(sm.isValidatorOnline) == 0 {
		return false
	}
	for _, b := range sm.isValidatorOnline {
		if !b {
			return false
		}
	}
	return true
}
