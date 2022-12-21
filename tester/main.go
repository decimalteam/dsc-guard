package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"bitbucket.org/decimalteam/dsc-guard/guard"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

type stubGuard struct {
	logger tmlog.Logger
}

var _ guard.Guarder = &stubGuard{}

func newStubGuard(logger tmlog.Logger) *stubGuard {
	return &stubGuard{
		logger: logger,
	}
}

func (sg *stubGuard) ReportWatcher(id string, state guard.WatcherState) {
	sg.logger.Debug(fmt.Sprintf("ReportWatcher(%s) state=%d", id, state))
}

func (sg *stubGuard) ReportTxValidity(id string, valid bool) {
	sg.logger.Debug(fmt.Sprintf("ReportTxValidity(%s) valid=%v", id, valid))
}
func (sg *stubGuard) ReportValidatorOnline(id string, height int64, online bool) {
	sg.logger.Debug(fmt.Sprintf("ReportValidatorOnline(%s) height=%d online=%v", id, height, online))
}

func (sg *stubGuard) SetSign(height int64, signed bool) {
	sg.logger.Debug(fmt.Sprintf("SetSign height=%d signed=%v", height, signed))
}

func main() {
	logger := tmlog.NewTMLogger(os.Stdout)
	// http://localhost:26657
	// https://devnet-dec2-node-01.decimalchain.com/rpc/
	w := guard.NewWatcher("tcp://91.219.30.111:26657", guard.Config{
		FallbackPause:    1,
		NewBlockTimeout:  100,
		ValidatorAddress: "98856A63A95E740D65ACFF64BB920C59B2ABB4C4",
	}, newStubGuard(logger), logger, guard.NewCooldownLock(time.Second))
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		w.Start()
		wg.Done()
	}()
	wg.Wait()
}
