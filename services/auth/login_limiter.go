package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLimiterUnavailable signals that Redis is configured but unreachable. The
// login flow must fail closed (reject) rather than silently disable rate limiting.
var ErrLimiterUnavailable = errors.New("login limiter unavailable")

type LoginLimiter struct {
	rdb *redis.Client
}

func NewLoginLimiter(rdb *redis.Client) *LoginLimiter {
	return &LoginLimiter{rdb: rdb}
}

func (l *LoginLimiter) CheckIP(ctx context.Context, ip string) error {
	if l == nil || l.rdb == nil {
		return nil // limiter disabled (no Redis configured)
	}
	key := "ratelimit:login:ip:" + ip
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLimiterUnavailable, err)
	}
	if count == 1 {
		l.rdb.Expire(ctx, key, 1*time.Minute)
	}
	if count > 10 {
		return fmt.Errorf("too many login attempts from this IP")
	}
	return nil
}

func (l *LoginLimiter) RecordFailure(ctx context.Context, username string) (failCount int, locked bool) {
	key := "login:fail:" + username
	count, _ := l.rdb.Incr(ctx, key).Result()
	l.rdb.Expire(ctx, key, 15*time.Minute)

	fc := int(count)
	if fc >= 5 {
		l.rdb.Set(ctx, "lockout:"+username, "1", 15*time.Minute)
		return fc, true
	}
	return fc, false
}

// IsLocked reports whether the account is locked out. A Redis error is returned
// so the caller can fail closed instead of treating the account as unlocked.
func (l *LoginLimiter) IsLocked(ctx context.Context, username string) (bool, error) {
	if l == nil || l.rdb == nil {
		return false, nil
	}
	exists, err := l.rdb.Exists(ctx, "lockout:"+username).Result()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrLimiterUnavailable, err)
	}
	return exists > 0, nil
}

func (l *LoginLimiter) ClearFailures(ctx context.Context, username string) {
	l.rdb.Del(ctx, "login:fail:"+username, "lockout:"+username)
}

func (l *LoginLimiter) GetStatus(ctx context.Context, ip string, username string) (failCount int, requireCaptcha bool, locked bool) {
	ipKey := "login:fail:ip:" + ip
	ipCount, _ := l.rdb.Get(ctx, ipKey).Int()

	userKey := "login:fail:" + username
	userCount, _ := l.rdb.Get(ctx, userKey).Int()

	totalFail := ipCount
	if userCount > totalFail {
		totalFail = userCount
	}

	requireCaptcha = totalFail >= 3
	locked, err := l.IsLocked(ctx, username)
	if err != nil {
		locked = true // fail closed when the lockout state cannot be read
	}
	return totalFail, requireCaptcha, locked
}

func (l *LoginLimiter) RecordIPFailure(ctx context.Context, ip string) {
	key := "login:fail:ip:" + ip
	l.rdb.Incr(ctx, key)
	l.rdb.Expire(ctx, key, 15*time.Minute)
}
