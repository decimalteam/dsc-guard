package guard

type GlobalState = uint
type WatcherState = uint
type TxState = uint

const (
	StateStarting GlobalState = iota
	StateConnecting
	StateWatching           // watching, transaction data is valid, validator is online
	StateValidatorIsOffline // watching, transaction data is valid, validator is offline
	StateWatchingWithoutTx  // watching, transaction data is invalid, validator in any state
)

const (
	WatcherConnecting WatcherState = iota
	WatcherQueryValidator
	WatcherWatching
)

const (
	TxUnknown TxState = iota
	TxInvalid
	TxValid
)
