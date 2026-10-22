// Package safe provides goroutine helpers that recover panics and record the
// stack trace, so a background failure cannot terminate the process silently.
package safe

import (
	"runtime/debug"

	"go.uber.org/zap"
)

// Go runs fn in a goroutine and recovers panics. A nil logger is tolerated.
func Go(logger *zap.Logger, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Error("panic recovered in background goroutine",
						zap.Any("panic", r),
						zap.ByteString("stack", debug.Stack()))
				}
			}
		}()
		fn()
	}()
}
