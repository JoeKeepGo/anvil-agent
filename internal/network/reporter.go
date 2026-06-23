package network

import "context"

// StateReporter adapts a Detector and the host's accepted identity into the
// trusted /agent/v1/network/state report. It holds no product state.
type StateReporter struct {
	detector *Detector
	identity AgentSummary
}

// NewStateReporter returns a StateReporter bound to the given detector and
// host identity.
func NewStateReporter(detector *Detector, identity AgentSummary) *StateReporter {
	return &StateReporter{detector: detector, identity: identity}
}

// NetworkState builds the trusted network state report.
func (r *StateReporter) NetworkState(ctx context.Context) (NetworkState, error) {
	return r.detector.NetworkState(ctx, r.identity)
}
