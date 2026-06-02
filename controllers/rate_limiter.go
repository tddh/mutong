package controllers

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type tokenBucket struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	maxTokens  int
	refillRate time.Duration
	stopCh     chan struct{}
}

type bucket struct {
	tokens     int
	lastRefill time.Time
}

func newTokenBucket(maxTokens int, refillRate time.Duration) *tokenBucket {
	tb := &tokenBucket{
		buckets:    make(map[string]*bucket),
		maxTokens:  maxTokens,
		refillRate: refillRate,
		stopCh:     make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-tb.stopCh:
				return
			case <-ticker.C:
				tb.mu.Lock()
				for ip, b := range tb.buckets {
					if time.Since(b.lastRefill) > 10*time.Minute {
						delete(tb.buckets, ip)
					}
				}
				tb.mu.Unlock()
			}
		}
	}()
	return tb
}

func (tb *tokenBucket) allow(ip string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	b, exists := tb.buckets[ip]
	if !exists {
		tb.buckets[ip] = &bucket{tokens: tb.maxTokens - 1, lastRefill: time.Now()}
		return true
	}

	elapsed := time.Since(b.lastRefill)
	tokensToAdd := int(elapsed / tb.refillRate)
	if tokensToAdd > 0 {
		b.tokens += tokensToAdd
		if b.tokens > tb.maxTokens {
			b.tokens = tb.maxTokens
		}
		b.lastRefill = b.lastRefill.Add(time.Duration(tokensToAdd) * tb.refillRate)
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// Stop 停止 tokenBucket 后台清理协程
func (tb *tokenBucket) Stop() {
	close(tb.stopCh)
}

func RateLimitMiddleware(maxRequests int, window time.Duration) gin.HandlerFunc {
	rl := newTokenBucket(maxRequests, window)
	return func(ctx *gin.Context) {
		if !rl.allow(ctx.ClientIP()) {
			ctx.AbortWithStatusJSON(429, map[string]interface{}{
				"error": "too many requests, please try again later",
			})
			return
		}
		ctx.Next()
	}
}
