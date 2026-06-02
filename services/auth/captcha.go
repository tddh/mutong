package auth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
)

type CaptchaData struct {
	ID    string `json:"captcha_id"`
	Image string `json:"image"`
	Thumb string `json:"thumb"`
}

type CaptchaService struct {
	rdb     *redis.Client
	captcha click.Captcha
}

func NewCaptchaService(rdb *redis.Client) *CaptchaService {
	builder := click.NewBuilder(
		click.WithRangeLen(option.RangeVal{Min: 3, Max: 3}),
		click.WithRangeColors([]string{
			"#1B5E20", "#B71C1C", "#0D47A1", "#4A148C", "#E65100",
		}),
	)
	return &CaptchaService{
		rdb:     rdb,
		captcha: builder.Make(),
	}
}

func (s *CaptchaService) Generate(ctx context.Context) (*CaptchaData, error) {
	data, err := s.captcha.Generate()
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()
	answer, _ := json.Marshal(data.GetData())
	s.rdb.Set(ctx, "captcha:"+id, answer, 5*time.Minute)

	masterB64, _ := data.GetMasterImage().ToBase64()
	thumbB64, _ := data.GetThumbImage().ToBase64()

	return &CaptchaData{
		ID:    id,
		Image: masterB64,
		Thumb: thumbB64,
	}, nil
}

func (s *CaptchaService) Verify(ctx context.Context, id string, userDotsJSON string) bool {
	stored, err := s.rdb.Get(ctx, "captcha:"+id).Result()
	if err != nil {
		return false
	}
	s.rdb.Del(ctx, "captcha:"+id)

	var expected map[int]*click.Dot
	if err := json.Unmarshal([]byte(stored), &expected); err != nil {
		return false
	}

	var actual []*click.Dot
	if err := json.Unmarshal([]byte(userDotsJSON), &actual); err != nil {
		return false
	}

	if len(actual) != len(expected) {
		return false
	}

	for i, userDot := range actual {
		exp, ok := expected[i]
		if !ok {
			return false
		}
		if !click.Validate(userDot.X, userDot.Y, exp.X, exp.Y, exp.Width, exp.Height, 0) {
			return false
		}
	}

	return true
}
