package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/allegro/bigcache/v3"
	"go.uber.org/zap"
)

type ResourceCache struct {
	cache     *bigcache.BigCache
	logger    interfaces.Logger
	trackedMu sync.RWMutex
	tracked   map[string]bool
}

type CacheEntry struct {
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

func NewResourceCache(logger interfaces.Logger, lifeTimeMinutes int, cleanWindowMinutes int, hardMaxCacheSizeMB int, enabled bool) (*ResourceCache, error) {
	if !enabled {
		logger.Info("Resource cache disabled")
		return &ResourceCache{logger: logger, tracked: make(map[string]bool)}, nil
	}

	config := bigcache.DefaultConfig(time.Duration(lifeTimeMinutes) * time.Minute)
	config.CleanWindow = time.Duration(cleanWindowMinutes) * time.Minute
	config.HardMaxCacheSize = hardMaxCacheSizeMB
	config.Logger = &bigCacheLogger{logger: logger}

	cache, err := bigcache.New(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource cache: %w", err)
	}

	logger.Info("Resource cache initialized",
		zap.Int("lifeTimeMinutes", lifeTimeMinutes),
		zap.Int("cleanWindowMinutes", cleanWindowMinutes),
		zap.Int("hardMaxCacheSizeMB", hardMaxCacheSizeMB))

	return &ResourceCache{
		cache:   cache,
		logger:  logger,
		tracked: make(map[string]bool),
	}, nil
}

func (c *ResourceCache) Get(key string) (interface{}, bool) {
	if c.cache == nil {
		return nil, false
	}

	data, err := c.cache.Get(key)
	if err != nil {
		return nil, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.logger.Warn("Failed to unmarshal cache entry", zap.String("key", key), zap.Error(err))
		return nil, false
	}

	return entry.Data, true
}

func (c *ResourceCache) Set(key string, data interface{}) error {
	if c.cache == nil {
		return nil
	}

	entry := CacheEntry{
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		c.logger.Error("Failed to marshal cache entry", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	c.trackedMu.Lock()
	c.tracked[key] = true
	c.trackedMu.Unlock()

	return c.cache.Set(key, jsonData)
}

func (c *ResourceCache) Delete(key string) error {
	if c.cache == nil {
		return nil
	}
	c.trackedMu.Lock()
	delete(c.tracked, key)
	c.trackedMu.Unlock()
	return c.cache.Delete(key)
}

func (c *ResourceCache) ClearAll() {
	if c.cache == nil {
		return
	}
	c.trackedMu.RLock()
	keys := make([]string, 0, len(c.tracked))
	for k := range c.tracked {
		keys = append(keys, k)
	}
	c.trackedMu.RUnlock()

	for _, key := range keys {
		_ = c.cache.Delete(key)
	}
	c.trackedMu.Lock()
	c.tracked = make(map[string]bool)
	c.trackedMu.Unlock()
	c.logger.Debug("Resource cache cleared")
}

type bigCacheLogger struct {
	logger interfaces.Logger
}

func (l *bigCacheLogger) Printf(format string, v ...interface{}) {
	l.logger.Debug("BigCache", zap.String("msg", fmt.Sprintf(format, v...)))
}
