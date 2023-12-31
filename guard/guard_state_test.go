package guard

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

// dummyWriter implement io.Writer
type dummyWriter struct{}

func (d dummyWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func TestGuardStateTransition(t *testing.T) {
	gsm := NewGuardState(tmlog.NewTMLogger(dummyWriter{}), Config{}, nil)
	// check connecting
	require.Equal(t, StateStarting, gsm.state)
	gsm.ProcessEvent(eventWatcherState{"a", WatcherConnecting})
	gsm.ProcessEvent(eventWatcherState{"b", WatcherConnecting})
	require.Equal(t, StateConnecting, gsm.state)
	gsm.ProcessEvent(eventWatcherState{"a", WatcherQueryValidator})
	gsm.ProcessEvent(eventWatcherState{"b", WatcherQueryValidator})
	require.Equal(t, StateConnecting, gsm.state)

	// watcher is up, but tx is not checked (=invalid), validator state unknown (=offline)
	gsm.ProcessEvent(eventWatcherState{"a", WatcherWatching})
	require.Equal(t, StateWatchingWithoutTx, gsm.state)

	// tx checked, validator state unknown (=offline)
	gsm.ProcessEvent(eventTxValidity{"a", true})
	require.Equal(t, StateValidatorIsOffline, gsm.state)

	// validator state known = online
	gsm.ProcessEvent(eventValidatorState{"b", 1, true})
	require.Equal(t, StateWatching, gsm.state)

	// tx now invalid (someone uses mnemonic etc...)
	gsm.ProcessEvent(eventTxValidity{"a", false})
	require.Equal(t, StateWatchingWithoutTx, gsm.state)

	// tx checked, validator online
	gsm.ProcessEvent(eventTxValidity{"a", true})
	require.Equal(t, StateWatching, gsm.state)

	// validator now is offline by unknown reason
	gsm.ProcessEvent(eventValidatorState{"b", 2, false})
	require.Equal(t, StateValidatorIsOffline, gsm.state)
}

func TestGuardStateTxTrigger(t *testing.T) {
	var isOfflineTriggered = false
	gsm := NewGuardState(tmlog.NewTMLogger(dummyWriter{}), Config{}, func() {
		isOfflineTriggered = true
	})
	// check connecting
	require.Equal(t, StateStarting, gsm.state)
	gsm.ProcessEvent(eventWatcherState{"a", WatcherWatching})
	gsm.ProcessEvent(eventValidatorState{"b", 1, true})
	gsm.ProcessEvent(eventTxValidity{"a", true})
	require.Equal(t, StateWatching, gsm.state)
	// trigger offline
	gsm.ProcessEvent(eventValidatorSkipSign{})
	require.True(t, isOfflineTriggered)
	require.Equal(t, StateStarting, gsm.state)
}

func TestGuardRun(t *testing.T) {
	var isOfflineTriggered = false
	gsm := NewGuardState(tmlog.NewTMLogger(dummyWriter{}), Config{MissedBlocksLimit: 8, MissedBlocksWindow: 24}, func() {
		isOfflineTriggered = true
	})
	// check connecting
	require.Equal(t, StateStarting, gsm.state)
	gsm.eventReadTimeout = time.Second
	wg := sync.WaitGroup{}
	gsm.isRunning = true
	gsm.state = StateStarting

	// like Start(), but with WaitGroup for correct testing
	go func() {
		for gsm.isRunning {
			select {
			case ev := <-gsm.eventChannel:
				{
					gsm.ProcessEvent(ev)
					wg.Done()
				}
			case <-time.After(gsm.eventReadTimeout):
				{
					continue
				}
			}

		}
	}()
	// connecting
	wg.Add(1)
	gsm.ReportWatcher("a", WatcherWatching)
	wg.Add(1)
	gsm.ReportWatcher("b", WatcherWatching)
	wg.Wait()
	require.Equal(t, StateWatchingWithoutTx, gsm.state)
	// watching: tx valid, validator online
	wg.Add(2)
	gsm.ReportTxValidity("a", true)
	gsm.ReportValidatorOnline("a", 1, true)
	wg.Add(2)
	gsm.ReportTxValidity("b", true)
	gsm.ReportValidatorOnline("b", 1, true)
	wg.Wait()
	require.Equal(t, StateWatching, gsm.state)
	// now skipping
	wg.Add(1)
	for i := int64(0); i < 8; i++ {
		gsm.SetSign(i+1, false)
	}
	wg.Wait()
	require.True(t, isOfflineTriggered)
	require.Equal(t, StateStarting, gsm.state)
	wg.Add(1)
	gsm.ReportValidatorOnline("a", 10, false)
	wg.Wait()
	require.Equal(t, StateValidatorIsOffline, gsm.state)
	gsm.Stop()
}
