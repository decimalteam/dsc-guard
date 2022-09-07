package main

import (
	"fmt"
	"os"
	"sync"

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
func (sg *stubGuard) ReportValidatorOnline(height int64, online bool) {
	sg.logger.Debug(fmt.Sprintf("ReportValidatorOnline height=%d online=%v", height, online))
}

func (sg *stubGuard) SetSign(height int64, signed bool) {
	sg.logger.Debug(fmt.Sprintf("SetSign height=%d signed=%v", height, signed))
}

func main() {
	logger := tmlog.NewTMLogger(os.Stdout)
	w := guard.NewWatcher("http://localhost:26657", guard.Config{
		FallbackPause:    1,
		NewBlockTimeout:  10,
		ValidatorAddress: "376A99CFC7F908454BD2C3032ED792E856565F6E",
	}, newStubGuard(logger), logger)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		w.Start()
		wg.Done()
	}()
	wg.Wait()
}
