package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoginLimiter struct {
	rdb *redis.Client
}

func NewLoginLimiter(rdb *redis.Client) *LoginLimiter {
	return &LoginLimiter{rdb: rdb}
}

func (l *LoginLimiter) CheckIP(ctx context.Context, ip string) error {
	key := "ratelimit:login:ip:" + ip
	count, _ := l.rdb.Incr(ctx, key).Result()
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

func (l *LoginLimiter) IsLocked(ctx context.Context, username string) bool {
	exists, _ := l.rdb.Exists(ctx, "lockout:"+username).Result()
	return exists > 0
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
	locked = l.IsLocked(ctx, username)
	return totalFail, requireCaptcha, locked
}

func (l *LoginLimiter) RecordIPFailure(ctx context.Context, ip string) {
	key := "login:fail:ip:" + ip
	l.rdb.Incr(ctx, key)
	l.rdb.Expire(ctx, key, 15*time.Minute)
}
