package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// unreachableRedis returns a client pointed at a port with no listener so every
// command fails fast, exercising the fail-closed path without a real Redis.
func unreachableRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestLoginLimiterDisabledWithoutRedis(t *testing.T) {
	l := NewLoginLimiter(nil)
	if err := l.CheckIP(context.Background(), "1.2.3.4"); err != nil {
		t.Fatalf("disabled limiter must allow, got %v", err)
	}
	locked, err := l.IsLocked(context.Background(), "alice")
	if err != nil || locked {
		t.Fatalf("disabled limiter must report unlocked, got locked=%v err=%v", locked, err)
	}

	// A nil limiter is also treated as disabled.
	var nilLimiter *LoginLimiter
	if err := nilLimiter.CheckIP(context.Background(), "1.2.3.4"); err != nil {
		t.Fatalf("nil limiter must allow, got %v", err)
	}
}

func TestLoginLimiterFailsClosedOnRedisError(t *testing.T) {
	l := NewLoginLimiter(unreachableRedis(t))

	if err := l.CheckIP(context.Background(), "1.2.3.4"); !errors.Is(err, ErrLimiterUnavailable) {
		t.Fatalf("CheckIP expected ErrLimiterUnavailable, got %v", err)
	}
	if _, err := l.IsLocked(context.Background(), "alice"); !errors.Is(err, ErrLimiterUnavailable) {
		t.Fatalf("IsLocked expected ErrLimiterUnavailable, got %v", err)
	}
	// GetStatus must fail closed (locked) when the lockout state is unreadable.
	if _, _, locked := l.GetStatus(context.Background(), "1.2.3.4", "alice"); !locked {
		t.Fatal("GetStatus must report locked when Redis is unavailable")
	}
}
