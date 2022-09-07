package guard

type eventWatcherState struct {
	node  string
	state WatcherState
}

type eventTxValidity struct {
	node  string
	valid bool
}

type eventValidatorState struct {
	node   string
	online bool
}

type eventValidatorSkipSign struct{}
