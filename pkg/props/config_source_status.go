package props

// ConfigSourceState is how a declared config source slot fared when the root
// built the store.
type ConfigSourceState string

const (
	// ConfigSourceBuilt is a slot whose backend is a layer of the store.
	ConfigSourceBuilt ConfigSourceState = "built"
	// ConfigSourceUnconfigured is an optional slot nobody configured, left out.
	ConfigSourceUnconfigured ConfigSourceState = "not configured"
	// ConfigSourceUnavailable is an optional slot whose backend could not be
	// built, left out.
	ConfigSourceUnavailable ConfigSourceState = "unavailable"
)

// ConfigSourceStatus records one declared slot's outcome (spec 0204 D10), so
// doctor can report the stack without building it again. A required slot that
// fails stops the root instead, so it never has a status.
type ConfigSourceStatus struct {
	Slot  ConfigSource
	State ConfigSourceState
	// Writable and Sensitive are the built layer's, after the slot's own
	// writable setting and the kind's default.
	Writable  bool
	Sensitive bool
	// Credential names the link of the slot's credential chain that answered,
	// as its factory reported it; empty when the kind reports none.
	Credential string
	// Err is why an unavailable slot was left out.
	Err string
}
