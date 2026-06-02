//go:build lumberjack
// +build lumberjack

package config

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lj "gopkg.in/natefinch/lumberjack.v2"
)

// newRotatingLogger creates a lumberjack-based writer wrapped for zap
func newRotatingLogger(filename string, maxSize int, maxAge int) (zapcore.WriteSyncer, error) {
	if filename == "" {
		return nil, nil
	}
	cfg := lj.Logger{
		Filename: filename,
		MaxSize:  maxSize,
		MaxAge:   maxAge,
		Compress: true,
	}
	return zapcore.AddSync(&cfg), nil
}
