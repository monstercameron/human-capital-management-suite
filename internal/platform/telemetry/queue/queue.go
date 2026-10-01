// Package queue owns bounded telemetry admission, retry and shutdown
// semantics. It has no authority over business commits or retry budgets.
package queue

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

func Explain() string { return "bounded telemetry queue retry backpressure and shutdown contract" }

type Priority uint8

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityCritical
)

type DropReason string

const (
	DropQueueFull           DropReason = "QUEUE_FULL"
	DropDuplicate           DropReason = "DUPLICATE"
	DropRetryExhausted      DropReason = "RETRY_BUDGET_EXHAUSTED"
	DropShutdownTimeout     DropReason = "SHUTDOWN_TIMEOUT"
	DropExporterUnavailable DropReason = "EXPORTER_UNAVAILABLE"
)

type Status string

const (
	StatusComplete Status = "COMPLETE"
	StatusPartial  Status = "PARTIAL"
	StatusTimedOut Status = "TIMED_OUT"
)

type Item struct {
	ID       string
	Priority Priority
	Digest   string
}

type Config struct {
	Capacity    int
	RetryBudget int
}

type SubmitResult struct {
	Accepted bool
	Dropped  bool
	Reason   DropReason
}

type Health struct {
	Accepted    int
	Exported    int
	Dropped     int
	Retries     int
	QueueDepth  int
	Degraded    bool
	DropReasons map[DropReason]int
}

type Receipt struct {
	Status       Status
	Accepted     int
	Exported     int
	Dropped      int
	Retries      int
	DropReasons  map[DropReason]int
	QueueAtClose int
}

type Exporter func(Item) error

type Pipeline struct {
	mu        sync.Mutex
	config    Config
	items     []Item
	seen      map[string]bool
	health    Health
	accepting bool
}

var (
	ErrInvalidConfig = errors.New("telemetry queue: invalid configuration")
	ErrInvalidItem   = errors.New("telemetry queue: invalid item")
)

func New(config Config) (*Pipeline, error) {
	if config.Capacity <= 0 || config.RetryBudget < 0 {
		return nil, ErrInvalidConfig
	}
	return &Pipeline{config: config, seen: make(map[string]bool), accepting: true, health: Health{DropReasons: make(map[DropReason]int)}}, nil
}

func (p *Pipeline) Submit(item Item) (SubmitResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Digest) == "" || item.Priority > PriorityCritical {
		return SubmitResult{}, ErrInvalidItem
	}
	if !p.accepting {
		return p.dropLocked(DropShutdownTimeout), nil
	}
	if p.seen[item.ID] {
		return p.dropLocked(DropDuplicate), nil
	}
	if len(p.items) >= p.config.Capacity {
		lowest := 0
		for i := range p.items {
			if p.items[i].Priority < p.items[lowest].Priority {
				lowest = i
			}
		}
		if p.items[lowest].Priority >= item.Priority {
			return p.dropLocked(DropQueueFull), nil
		}
		p.removeAt(lowest)
		p.recordDrop(DropQueueFull)
	}
	p.items = append(p.items, item)
	p.seen[item.ID] = true
	p.health.Accepted++
	p.health.QueueDepth = len(p.items)
	return SubmitResult{Accepted: true}, nil
}

func (p *Pipeline) Flush(exporter Exporter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushLocked(exporter)
}

func (p *Pipeline) flushLocked(exporter Exporter) {
	for len(p.items) > 0 {
		item := p.items[0]
		p.items = p.items[1:]
		succeeded := false
		for attempt := 0; attempt <= p.config.RetryBudget; attempt++ {
			if exporter == nil {
				break
			}
			if attempt > 0 {
				p.health.Retries++
			}
			if exporter(item) == nil {
				succeeded = true
				break
			}
		}
		if succeeded {
			p.health.Exported++
		} else if exporter == nil {
			p.recordDrop(DropExporterUnavailable)
		} else {
			p.recordDrop(DropRetryExhausted)
		}
	}
	p.health.QueueDepth = len(p.items)
}

// Shutdown applies stop-admit, drain, stop-produce, flush, record and close in
// that order. The caller supplies time so a test and a production coordinator
// can use the same deterministic contract.
func (p *Pipeline) Shutdown(exporter Exporter, now, deadline time.Time) Receipt {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.accepting = false
	if now.IsZero() {
		now = time.Now().UTC()
	}
	receipt := Receipt{Accepted: p.health.Accepted, DropReasons: make(map[DropReason]int)}
	if !now.Before(deadline) {
		for len(p.items) > 0 {
			p.items = p.items[1:]
			p.recordDrop(DropShutdownTimeout)
		}
		receipt.Status = StatusTimedOut
	} else {
		p.flushLocked(exporter)
		receipt.Status = StatusComplete
		if p.health.Dropped > 0 {
			receipt.Status = StatusPartial
		}
	}
	p.health.QueueDepth = len(p.items)
	receipt.Exported = p.health.Exported
	receipt.Dropped = p.health.Dropped
	receipt.Retries = p.health.Retries
	receipt.QueueAtClose = len(p.items)
	for reason, count := range p.health.DropReasons {
		receipt.DropReasons[reason] = count
	}
	return receipt
}

func (p *Pipeline) Health() Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := p.health
	result.DropReasons = make(map[DropReason]int, len(p.health.DropReasons))
	for reason, count := range p.health.DropReasons {
		result.DropReasons[reason] = count
	}
	result.QueueDepth = len(p.items)
	result.Degraded = result.Dropped > 0 || result.QueueDepth > 0
	return result
}

func (p *Pipeline) dropLocked(reason DropReason) SubmitResult {
	p.recordDrop(reason)
	return SubmitResult{Dropped: true, Reason: reason}
}

func (p *Pipeline) recordDrop(reason DropReason) {
	p.health.Dropped++
	p.health.Degraded = true
	p.health.DropReasons[reason]++
}

func (p *Pipeline) removeAt(index int) {
	copy(p.items[index:], p.items[index+1:])
	p.items = p.items[:len(p.items)-1]
}

// ExplainReceipt is stable and contains counts only, never signal identifiers.
func ExplainReceipt(receipt Receipt) string {
	reasons := make([]string, 0, len(receipt.DropReasons))
	for reason, count := range receipt.DropReasons {
		reasons = append(reasons, fmt.Sprintf("%s=%d", reason, count))
	}
	sort.Strings(reasons)
	return fmt.Sprintf("telemetry shutdown %s accepted=%d exported=%d dropped=%d retries=%d reasons=%s", receipt.Status, receipt.Accepted, receipt.Exported, receipt.Dropped, receipt.Retries, strings.Join(reasons, ","))
}
