package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"bitbucket.org/decimalteam/dsc-guard/guard"
	"github.com/spf13/viper"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

func main() {
	var watchers []*guard.Watcher
	var wg sync.WaitGroup

	logger := tmlog.NewTMLogger(os.Stdout)

	viper.SetConfigFile(".env")
	err := viper.ReadInConfig()
	if err != nil {
		logger.Error(fmt.Sprintf("viper.ReadInConfig error: %s", err.Error()))
		os.Exit(1)
	}

	config := guard.Config{}
	err = viper.Unmarshal(&config)
	if err != nil {
		logger.Error(fmt.Sprintf("viper.Unmarshal error: %s", err.Error()))
		os.Exit(1)
	}

	txData, err := hex.DecodeString(config.SetOfflineTx)
	if err != nil {
		logger.Error(fmt.Sprintf("can't decode tx data: %s", err.Error()))
	}

	logger.Info("Start DSC guard")

	gsm := guard.NewGuardState(logger, config, func() {
		for _, w := range watchers {
			w.SendOffline()
		}
	})

	wg.Add(1)
	go func() {
		gsm.Start()
		wg.Done()
	}()

	nodes := strings.Split(config.NodesEndpoints, ",")
	for _, node := range nodes {
		w := guard.NewWatcher(
			node,
			config,
			gsm,
			logger,
		)
		w.SetTxData(txData)
		wg.Add(1)
		go func() {
			w.Start()
			wg.Done()
		}()
		watchers = append(watchers, w)
	}

	// TODO: add http endpoint for transaction dynamic update

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)

	// wait for stop/restart/etc
	<-exit

	// graceful shotdown
	for _, w := range watchers {
		w.Stop()
	}
	gsm.Stop()

	wg.Wait()
}
