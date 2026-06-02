package inspection

import "time"

// TrendPoint represents a daily trend data point for inspection issues.
type TrendPoint struct {
	Date  time.Time `json:"date"`
	Count int       `json:"count"`
}
