package interfaces

import "time"

// TraceQuerier provides distributed tracing query capabilities
type TraceQuerier interface {
	// QueryTraces fetch traces for a given service and operation, optionally filtered by traceId
	QueryTraces(serviceName, operationName, traceId string, limit int) ([]Trace, error)
	// GetSpans returns all spans for a given traceId
	GetSpans(traceId string) ([]Span, error)
	// GetServices returns a list of available service names
	GetServices() ([]string, error)
}

// Trace represents a trace with its spans
type Trace struct {
	TraceID   string
	Spans     []Span
	Duration  int64     // total duration in milliseconds
	StartTime time.Time // trace start time
}

// Span represents a single span within a trace
type Span struct {
	SpanID        string
	TraceID       string
	ParentSpanID  string
	OperationName string
	ServiceName   string
	StartTime     time.Time
	Duration      int64
	Tags          map[string]string
	Status        string
}
