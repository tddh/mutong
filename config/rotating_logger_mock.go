//go:build !lumberjack
// +build !lumberjack

package config

import (
	"os"

	"go.uber.org/zap/zapcore"
)

// newRotatingLogger fallback when lumberjack is not enabled
func newRotatingLogger(filename string, maxSize int, maxAge int) (zapcore.WriteSyncer, error) {
	if filename == "" {
		return nil, nil
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return zapcore.AddSync(f), nil
}
