package diagnosis

import "testing"

// Stop 在 graceful shutdown 链路被真实调用（FUNC-01 修复后），
// stopCh 未初始化会 close(nil channel) panic。
func TestEngineStopIsSafe(t *testing.T) {
	e := NewEngine(nil, nil, nil)
	e.Stop()
	e.Stop()
}
