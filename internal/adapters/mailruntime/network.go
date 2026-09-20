package mailruntime

import "context"

// EnsureNetwork creates only the fixed role network and is exposed only by the
// control-plane adapter. The controller must journal intent before calling it.
func (value *ControlPlane) EnsureNetwork(ctx context.Context) (NetworkSummary, error) {
	adapter := value.adapter
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return NetworkSummary{}, ErrClosed
	}
	bounded, cancel, err := adapter.boundedContext(ctx)
	if err != nil {
		return NetworkSummary{}, err
	}
	defer cancel()
	if summary, exists, err := adapter.inspectNetwork(bounded); err != nil {
		return NetworkSummary{}, err
	} else if exists {
		return summary, nil
	}
	if _, err := adapter.run(bounded, commandRequest{kind: commandNetworkCreate, name: adapter.network}, false); err != nil {
		return NetworkSummary{}, ErrRecoveryRequired
	}
	summary, exists, err := adapter.inspectNetwork(bounded)
	if err != nil || !exists {
		return NetworkSummary{}, ErrRecoveryRequired
	}
	return summary, nil
}
