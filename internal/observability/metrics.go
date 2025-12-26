package observability

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds all performance metrics for the TurboCI-Lite service.
type Metrics struct {
	// Database metrics
	dbLockWaitTime       *Histogram
	dbTransactionBegin   *Histogram
	dbTransactionCommit  *Histogram
	dbQueryDuration      *HistogramVec
	dbActiveTransactions *AtomicGauge

	// Dispatcher metrics
	dispatchCycleDuration *HistogramVec
	dispatcherQueueDepth  *GaugeVec
	dispatchLatency       *HistogramVec
	pollLoopCycleDuration *HistogramVec
	pollLoopIdleTime      *HistogramVec

	// Service layer metrics
	writeNodesDuration       *Histogram
	queryNodesDuration       *Histogram
	dependencyResolutionTime *Histogram
	nodesEvaluated           *CounterVec
}

// NewMetrics creates a new Metrics instance with all metrics initialized.
func NewMetrics() *Metrics {
	return &Metrics{
		// Database metrics
		dbLockWaitTime:       NewHistogram(),
		dbTransactionBegin:   NewHistogram(),
		dbTransactionCommit:  NewHistogram(),
		dbQueryDuration:      NewHistogramVec(),
		dbActiveTransactions: NewAtomicGauge(),

		// Dispatcher metrics
		dispatchCycleDuration: NewHistogramVec(),
		dispatcherQueueDepth:  NewGaugeVec(),
		dispatchLatency:       NewHistogramVec(),
		pollLoopCycleDuration: NewHistogramVec(),
		pollLoopIdleTime:      NewHistogramVec(),

		// Service layer metrics
		writeNodesDuration:       NewHistogram(),
		queryNodesDuration:       NewHistogram(),
		dependencyResolutionTime: NewHistogram(),
		nodesEvaluated:           NewCounterVec(),
	}
}

// Database metrics accessors
func (m *Metrics) DBLockWaitTime() *Histogram              { return m.dbLockWaitTime }
func (m *Metrics) DBTransactionBegin() *Histogram          { return m.dbTransactionBegin }
func (m *Metrics) DBTransactionCommit() *Histogram         { return m.dbTransactionCommit }
func (m *Metrics) DBQueryDuration() *HistogramVec          { return m.dbQueryDuration }
func (m *Metrics) DBActiveTransactions() *AtomicGauge      { return m.dbActiveTransactions }

// Dispatcher metrics accessors
func (m *Metrics) DispatchCycleDuration() *HistogramVec { return m.dispatchCycleDuration }
func (m *Metrics) DispatcherQueueDepth() *GaugeVec      { return m.dispatcherQueueDepth }
func (m *Metrics) DispatchLatency() *HistogramVec       { return m.dispatchLatency }
func (m *Metrics) PollLoopCycleDuration() *HistogramVec { return m.pollLoopCycleDuration }
func (m *Metrics) PollLoopIdleTime() *HistogramVec      { return m.pollLoopIdleTime }

// Service layer metrics accessors
func (m *Metrics) WriteNodesDuration() *Histogram       { return m.writeNodesDuration }
func (m *Metrics) QueryNodesDuration() *Histogram       { return m.queryNodesDuration }
func (m *Metrics) DependencyResolutionTime() *Histogram { return m.dependencyResolutionTime }
func (m *Metrics) NodesEvaluated() *CounterVec          { return m.nodesEvaluated }

// Snapshot returns a snapshot of all metrics for reporting.
func (m *Metrics) Snapshot() *MetricsSnapshot {
	return &MetricsSnapshot{
		// Database metrics
		DBLockWaitTime:       m.dbLockWaitTime.Snapshot(),
		DBTransactionBegin:   m.dbTransactionBegin.Snapshot(),
		DBTransactionCommit:  m.dbTransactionCommit.Snapshot(),
		DBQueryDuration:      m.dbQueryDuration.Snapshot(),
		DBActiveTransactions: m.dbActiveTransactions.Get(),

		// Dispatcher metrics
		DispatchCycleDuration: m.dispatchCycleDuration.Snapshot(),
		DispatcherQueueDepth:  m.dispatcherQueueDepth.Snapshot(),
		DispatchLatency:       m.dispatchLatency.Snapshot(),
		PollLoopCycleDuration: m.pollLoopCycleDuration.Snapshot(),
		PollLoopIdleTime:      m.pollLoopIdleTime.Snapshot(),

		// Service layer metrics
		WriteNodesDuration:       m.writeNodesDuration.Snapshot(),
		QueryNodesDuration:       m.queryNodesDuration.Snapshot(),
		DependencyResolutionTime: m.dependencyResolutionTime.Snapshot(),
		NodesEvaluated:           m.nodesEvaluated.Snapshot(),
	}
}

// MetricsSnapshot holds a point-in-time snapshot of all metrics.
type MetricsSnapshot struct {
	// Database metrics
	DBLockWaitTime       HistogramSnapshot            `json:"db_lock_wait_time"`
	DBTransactionBegin   HistogramSnapshot            `json:"db_transaction_begin"`
	DBTransactionCommit  HistogramSnapshot            `json:"db_transaction_commit"`
	DBQueryDuration      map[string]HistogramSnapshot `json:"db_query_duration"`
	DBActiveTransactions int64                        `json:"db_active_transactions"`

	// Dispatcher metrics
	DispatchCycleDuration map[string]HistogramSnapshot `json:"dispatch_cycle_duration"`
	DispatcherQueueDepth  map[string]float64           `json:"dispatcher_queue_depth"`
	DispatchLatency       map[string]HistogramSnapshot `json:"dispatch_latency"`
	PollLoopCycleDuration map[string]HistogramSnapshot `json:"poll_loop_cycle_duration"`
	PollLoopIdleTime      map[string]HistogramSnapshot `json:"poll_loop_idle_time"`

	// Service layer metrics
	WriteNodesDuration       HistogramSnapshot `json:"write_nodes_duration"`
	QueryNodesDuration       HistogramSnapshot `json:"query_nodes_duration"`
	DependencyResolutionTime HistogramSnapshot `json:"dependency_resolution_time"`
	NodesEvaluated           map[string]int64  `json:"nodes_evaluated"`
}

// Histogram tracks the distribution of duration measurements.
// Thread-safe for concurrent observations.
type Histogram struct {
	mu     sync.RWMutex
	values []float64 // Stored in microseconds for precision
}

// NewHistogram creates a new histogram.
func NewHistogram() *Histogram {
	return &Histogram{
		values: make([]float64, 0, 1000),
	}
}

// Observe records a duration measurement.
func (h *Histogram) Observe(d time.Duration) {
	micros := float64(d.Microseconds())
	h.mu.Lock()
	h.values = append(h.values, micros)
	h.mu.Unlock()
}

// Snapshot returns a point-in-time snapshot with percentiles calculated.
func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.values) == 0 {
		return HistogramSnapshot{}
	}

	// Copy and sort for percentile calculation
	sorted := make([]float64, len(h.values))
	copy(sorted, h.values)
	sort.Float64s(sorted)

	// Calculate statistics
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	return HistogramSnapshot{
		Count: len(sorted),
		Mean:  time.Duration(mean) * time.Microsecond,
		P50:   time.Duration(percentile(sorted, 0.50)) * time.Microsecond,
		P95:   time.Duration(percentile(sorted, 0.95)) * time.Microsecond,
		P99:   time.Duration(percentile(sorted, 0.99)) * time.Microsecond,
		Max:   time.Duration(sorted[len(sorted)-1]) * time.Microsecond,
	}
}

// HistogramSnapshot holds calculated statistics for a histogram.
type HistogramSnapshot struct {
	Count int           `json:"count"`
	Mean  time.Duration `json:"mean"`
	P50   time.Duration `json:"p50"`
	P95   time.Duration `json:"p95"`
	P99   time.Duration `json:"p99"`
	Max   time.Duration `json:"max"`
}

// percentile calculates the p-th percentile from sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := p * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))

	if lower == upper {
		return sorted[lower]
	}

	// Linear interpolation
	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// HistogramVec is a collection of histograms with labels.
type HistogramVec struct {
	mu         sync.RWMutex
	histograms map[string]*Histogram
}

// NewHistogramVec creates a new histogram vector.
func NewHistogramVec() *HistogramVec {
	return &HistogramVec{
		histograms: make(map[string]*Histogram),
	}
}

// WithLabels returns a histogram for the given label string.
func (hv *HistogramVec) WithLabels(labels string) *Histogram {
	hv.mu.RLock()
	h, ok := hv.histograms[labels]
	hv.mu.RUnlock()

	if ok {
		return h
	}

	// Create new histogram
	hv.mu.Lock()
	defer hv.mu.Unlock()

	// Double-check after acquiring write lock
	if h, ok := hv.histograms[labels]; ok {
		return h
	}

	h = NewHistogram()
	hv.histograms[labels] = h
	return h
}

// Snapshot returns snapshots of all histograms.
func (hv *HistogramVec) Snapshot() map[string]HistogramSnapshot {
	hv.mu.RLock()
	defer hv.mu.RUnlock()

	snapshot := make(map[string]HistogramSnapshot, len(hv.histograms))
	for label, h := range hv.histograms {
		snapshot[label] = h.Snapshot()
	}
	return snapshot
}

// Counter is a monotonically increasing counter using atomic operations.
type Counter struct {
	value int64
}

// NewCounter creates a new counter.
func NewCounter() *Counter {
	return &Counter{}
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add adds the given value to the counter.
func (c *Counter) Add(delta int64) {
	atomic.AddInt64(&c.value, delta)
}

// Get returns the current value.
func (c *Counter) Get() int64 {
	return atomic.LoadInt64(&c.value)
}

// CounterVec is a collection of counters with labels.
type CounterVec struct {
	mu       sync.RWMutex
	counters map[string]*Counter
}

// NewCounterVec creates a new counter vector.
func NewCounterVec() *CounterVec {
	return &CounterVec{
		counters: make(map[string]*Counter),
	}
}

// WithLabels returns a counter for the given label string.
func (cv *CounterVec) WithLabels(labels string) *Counter {
	cv.mu.RLock()
	c, ok := cv.counters[labels]
	cv.mu.RUnlock()

	if ok {
		return c
	}

	// Create new counter
	cv.mu.Lock()
	defer cv.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := cv.counters[labels]; ok {
		return c
	}

	c = NewCounter()
	cv.counters[labels] = c
	return c
}

// Snapshot returns the current values of all counters.
func (cv *CounterVec) Snapshot() map[string]int64 {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	snapshot := make(map[string]int64, len(cv.counters))
	for label, c := range cv.counters {
		snapshot[label] = c.Get()
	}
	return snapshot
}

// AtomicGauge is a gauge that can be set and read atomically.
type AtomicGauge struct {
	value int64
}

// NewAtomicGauge creates a new atomic gauge.
func NewAtomicGauge() *AtomicGauge {
	return &AtomicGauge{}
}

// Set sets the gauge to the given value.
func (g *AtomicGauge) Set(val int64) {
	atomic.StoreInt64(&g.value, val)
}

// Inc increments the gauge by 1.
func (g *AtomicGauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

// Dec decrements the gauge by 1.
func (g *AtomicGauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

// Get returns the current value.
func (g *AtomicGauge) Get() int64 {
	return atomic.LoadInt64(&g.value)
}

// GaugeVec is a collection of gauges with labels.
type GaugeVec struct {
	mu     sync.RWMutex
	gauges map[string]float64
}

// NewGaugeVec creates a new gauge vector.
func NewGaugeVec() *GaugeVec {
	return &GaugeVec{
		gauges: make(map[string]float64),
	}
}

// Set sets the gauge for the given labels to the specified value.
func (gv *GaugeVec) Set(labels string, value float64) {
	gv.mu.Lock()
	gv.gauges[labels] = value
	gv.mu.Unlock()
}

// Snapshot returns the current values of all gauges.
func (gv *GaugeVec) Snapshot() map[string]float64 {
	gv.mu.RLock()
	defer gv.mu.RUnlock()

	snapshot := make(map[string]float64, len(gv.gauges))
	for label, value := range gv.gauges {
		snapshot[label] = value
	}
	return snapshot
}

// ServeHTTP implements http.Handler for metrics exposition.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snapshot := m.Snapshot()

	// Support both JSON and text format
	format := r.URL.Query().Get("format")
	if format == "json" || r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		encoder.Encode(snapshot)
		return
	}

	// Default: human-readable text format
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	fmt.Fprintf(w, "# TurboCI-Lite Performance Metrics\n\n")

	// Database metrics
	fmt.Fprintf(w, "## Database Metrics\n\n")
	writeHistogramSummary(w, "DB Lock Wait Time", snapshot.DBLockWaitTime)
	writeHistogramSummary(w, "DB Transaction Begin", snapshot.DBTransactionBegin)
	writeHistogramSummary(w, "DB Transaction Commit", snapshot.DBTransactionCommit)
	fmt.Fprintf(w, "DB Active Transactions: %d\n\n", snapshot.DBActiveTransactions)

	if len(snapshot.DBQueryDuration) > 0 {
		fmt.Fprintf(w, "DB Query Duration by type:\n")
		for label, hist := range snapshot.DBQueryDuration {
			fmt.Fprintf(w, "  %s:\n", label)
			writeHistogramSummaryIndented(w, hist)
		}
		fmt.Fprintf(w, "\n")
	}

	// Dispatcher metrics
	fmt.Fprintf(w, "## Dispatcher Metrics\n\n")

	if len(snapshot.DispatchCycleDuration) > 0 {
		fmt.Fprintf(w, "Dispatch Cycle Duration by runner type:\n")
		for label, hist := range snapshot.DispatchCycleDuration {
			fmt.Fprintf(w, "  %s:\n", label)
			writeHistogramSummaryIndented(w, hist)
		}
		fmt.Fprintf(w, "\n")
	}

	if len(snapshot.DispatcherQueueDepth) > 0 {
		fmt.Fprintf(w, "Dispatcher Queue Depth:\n")
		for label, value := range snapshot.DispatcherQueueDepth {
			fmt.Fprintf(w, "  %s: %.0f\n", label, value)
		}
		fmt.Fprintf(w, "\n")
	}

	if len(snapshot.DispatchLatency) > 0 {
		fmt.Fprintf(w, "Dispatch Latency by runner type:\n")
		for label, hist := range snapshot.DispatchLatency {
			fmt.Fprintf(w, "  %s:\n", label)
			writeHistogramSummaryIndented(w, hist)
		}
		fmt.Fprintf(w, "\n")
	}

	if len(snapshot.PollLoopCycleDuration) > 0 {
		fmt.Fprintf(w, "Poll Loop Cycle Duration by loop type:\n")
		for label, hist := range snapshot.PollLoopCycleDuration {
			fmt.Fprintf(w, "  %s:\n", label)
			writeHistogramSummaryIndented(w, hist)
		}
		fmt.Fprintf(w, "\n")
	}

	if len(snapshot.PollLoopIdleTime) > 0 {
		fmt.Fprintf(w, "Poll Loop Idle Time by loop type:\n")
		for label, hist := range snapshot.PollLoopIdleTime {
			fmt.Fprintf(w, "  %s:\n", label)
			writeHistogramSummaryIndented(w, hist)
		}
		fmt.Fprintf(w, "\n")
	}

	// Service layer metrics
	fmt.Fprintf(w, "## Service Layer Metrics\n\n")
	writeHistogramSummary(w, "WriteNodes Duration", snapshot.WriteNodesDuration)
	writeHistogramSummary(w, "QueryNodes Duration", snapshot.QueryNodesDuration)
	writeHistogramSummary(w, "Dependency Resolution Time", snapshot.DependencyResolutionTime)

	if len(snapshot.NodesEvaluated) > 0 {
		fmt.Fprintf(w, "\nNodes Evaluated by type:\n")
		for label, count := range snapshot.NodesEvaluated {
			fmt.Fprintf(w, "  %s: %d\n", label, count)
		}
	}
}

func writeHistogramSummary(w http.ResponseWriter, name string, h HistogramSnapshot) {
	if h.Count == 0 {
		fmt.Fprintf(w, "%s: no data\n", name)
		return
	}
	fmt.Fprintf(w, "%s (n=%d):\n", name, h.Count)
	fmt.Fprintf(w, "  Mean: %v, P50: %v, P95: %v, P99: %v, Max: %v\n",
		h.Mean, h.P50, h.P95, h.P99, h.Max)
}

func writeHistogramSummaryIndented(w http.ResponseWriter, h HistogramSnapshot) {
	if h.Count == 0 {
		fmt.Fprintf(w, "    no data\n")
		return
	}
	fmt.Fprintf(w, "    Count: %d, Mean: %v, P50: %v, P95: %v, P99: %v, Max: %v\n",
		h.Count, h.Mean, h.P50, h.P95, h.P99, h.Max)
}
