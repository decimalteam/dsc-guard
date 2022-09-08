package guard

// Config is an object containing validator guard configuration.
type Config struct {
	NodesEndpoints      string `mapstructure:"NODES_ENDPOINTS" mandatory:"true" default:"tcp://localhost:26657"`
	MissedBlocksLimit   int    `mapstructure:"MISSED_BLOCKS_LIMIT" mandatory:"true" default:"8"`
	MissedBlocksWindow  int    `mapstructure:"MISSED_BLOCKS_WINDOW" mandatory:"true" default:"24"`
	FallbackPause       int    `mapstructure:"FALLBACK_PAUSE" mandatory:"true" default:"2"`
	NewBlockTimeout     int    `mapstructure:"NEW_BLOCK_TIMEOUT" mandatory:"true" default:"10"`
	ValidatorAddress    string `mapstructure:"VALIDATOR_ADDRESS" mandatory:"true"`
	SetOfflineTx        string `mapstructure:"SET_OFFLINE_TX" mandatory:"true"`
	EnableGracePeriod   bool   `mapstructure:"ENABLE_GRACE_PERIOD" mandatory:"true" default:"true"`
	GracePeriodDuration int    `mapstructure:"GRACE_PERIOD_DURATION" mandatory:"true" default:"15840"`
}

const Subscriber = "watcher"

//const QueryNewBlock = "tm.events.type='NewBlock'"
const QueryNewBlock = "tm.event = 'NewBlock'"
const QueryValidatorSet = "tm.event = 'ValidatorSetUpdates'"
const ChannelCapacity = 1000 // 'buffer' (channel) capacity for events from tendermint
