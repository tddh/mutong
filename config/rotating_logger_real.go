//go:build lumberjack
// +build lumberjack

package config

import (
	"github.com/agentine/sawmill/compat"
	"go.uber.org/zap/zapcore"
)

// newRotatingLogger creates a sawmill-based writer wrapped for zap
func newRotatingLogger(filename string, maxSize int, maxAge int) (zapcore.WriteSyncer, error) {
	if filename == "" {
		return nil, nil
	}
	cfg := compat.Logger{
		Filename: filename,
		MaxSize:  maxSize,
		MaxAge:   maxAge,
		Compress: true,
	}
	return zapcore.AddSync(&cfg), nil
}
