package alert

import (
	"fmt"
	"sync"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

// AlertAggregator aggregates alerts into groups within a time window.
//   - groups: mapping from group key to AlertGroup
//   - mu: protects access to groups
//   - window: aggregation time window
type AlertAggregator struct {
	groups map[string]*AlertGroup
	mu     sync.RWMutex
	window time.Duration

	ticker *time.Ticker
	done   chan struct{}
}

// AlertGroup represents a collection of related alerts that should be notified together.
type AlertGroup struct {
	ID           string
	Alerts       []*alert_models.ProcessedAlert
	FirstSeen    time.Time
	LastSeen     time.Time
	Summary      string
	ResourceKind string
	Namespace    string
}

// NewAlertAggregator creates a new aggregator with the specified time window.
func NewAlertAggregator(window time.Duration) *AlertAggregator {
	return &AlertAggregator{
		groups: make(map[string]*AlertGroup),
		window: window,
		done:   make(chan struct{}),
	}
}

// GroupKey derives a grouping key for an alert.
// Priority: alertname (from labels) + namespace + owner (from enriched alert)
func (ag *AlertAggregator) GroupKey(pa *alert_models.ProcessedAlert) string {
	if pa == nil {
		return ""
	}
	var name, ns, owner string
	if pa.Labels != nil {
		if v := pa.Labels["alertname"]; v != "" {
			name = v
		}
	}
	if pa.Namespace != "" {
		ns = pa.Namespace
	}
	if pa.OwnerName != "" {
		owner = pa.OwnerName
	}
	return fmt.Sprintf("%s|%s|%s", name, ns, owner)
}

// Add inserts an alert into its corresponding group, creating the group if needed.
func (ag *AlertAggregator) Add(alert *alert_models.ProcessedAlert) {
	if alert == nil {
		return
	}
	key := ag.GroupKey(alert)
	if key == "" {
		// ignore alerts without a usable key
		return
	}
	ag.mu.Lock()
	defer ag.mu.Unlock()

	grp, ok := ag.groups[key]
	if !ok {
		grp = &AlertGroup{
			ID:        key,
			Alerts:    []*alert_models.ProcessedAlert{alert},
			FirstSeen: alert.StartsAt,
			LastSeen:  alert.StartsAt,
			Namespace: alert.Namespace,
		}
		if alert.ResourceType != "" {
			grp.ResourceKind = alert.ResourceType
		}
		ag.groups[key] = grp
		return
	}

	// append to existing group
	grp.Alerts = append(grp.Alerts, alert)
	// Update time bounds
	if !alert.StartsAt.IsZero() {
		if grp.FirstSeen.IsZero() || alert.StartsAt.Before(grp.FirstSeen) {
			grp.FirstSeen = alert.StartsAt
		}
		if alert.StartsAt.After(grp.LastSeen) {
			grp.LastSeen = alert.StartsAt
		}
	}
}

// Flush returns all current groups and clears the internal state.
func (ag *AlertAggregator) Flush() []*AlertGroup {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	groups := make([]*AlertGroup, 0, len(ag.groups))
	for _, g := range ag.groups {
		// enrich summary for the group if not already set
		if g.Summary == "" {
			g.Summary = fmt.Sprintf("%d alert(s) in namespace %s", len(g.Alerts), g.Namespace)
		}
		groups = append(groups, g)
	}
	// clear current groups after flushing
	ag.groups = make(map[string]*AlertGroup)
	return groups
}

// StartFlushScheduler starts a periodic flush with the given interval. The callback is
// invoked with the list of aggregated groups on each flush.
func (ag *AlertAggregator) StartFlushScheduler(interval time.Duration, callback func([]*AlertGroup)) {
	ag.mu.Lock()
	if ag.ticker != nil {
		ag.mu.Unlock()
		return
	}
	ag.window = interval
	ag.done = make(chan struct{})
	ag.ticker = time.NewTicker(interval)
	ag.mu.Unlock()

	go func() {
		for {
			select {
			case <-ag.ticker.C:
				groups := ag.Flush()
				if len(groups) > 0 && callback != nil {
					callback(groups)
				}
			case <-ag.done:
				return
			}
		}
	}()
}

// Stop stops the flush scheduler
func (ag *AlertAggregator) Stop() {
	ag.mu.Lock()
	if ag.ticker != nil {
		ag.ticker.Stop()
		ag.ticker = nil
	}
	if ag.done != nil {
		close(ag.done)
		ag.done = nil
	}
	ag.mu.Unlock()
}
