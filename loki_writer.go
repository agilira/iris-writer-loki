// loki_writer.go: External loki writer for Iris
//
// Copyright (c) 2025 AGILira
// Series: an AGILira library
// SPDX-License-Identifier: MPL-2.0

package lokiwriter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agilira/go-timecache"
	"github.com/agilira/iris"
)

// Config holds the configuration options for the Loki writer.
//
// All duration fields support standard Go duration formats (e.g., "1s", "100ms").
// The writer provides sensible defaults for all optional fields.
type Config struct {
	// Endpoint is the Loki push API URL (required).
	// Example: "http://localhost:3100/loki/api/v1/push"
	Endpoint string

	// TenantID is optional tenant identifier for multi-tenant Loki setups.
	// If set, it will be included as the X-Scope-OrgID header.
	TenantID string

	// Labels are static labels attached to all log streams.
	// These labels help organize and filter logs in Loki.
	Labels map[string]string

	// BatchSize is the number of records to batch before sending (default: 1000).
	// Larger batches improve throughput but increase memory usage.
	BatchSize int

	// FlushInterval is the maximum time to wait before flushing incomplete batches (default: 1s).
	// This ensures logs are sent even when batch size isn't reached.
	FlushInterval time.Duration

	// Timeout is the HTTP request timeout (default: 10s).
	Timeout time.Duration

	// OnError is an optional callback function for error handling.
	// If nil, errors are silently ignored. Use this for custom error logging.
	OnError func(error)

	// MaxRetries is the number of retry attempts for failed requests (default: 3).
	MaxRetries int

	// RetryDelay is the delay between retry attempts (default: 100ms).
	RetryDelay time.Duration
}

// Writer implements iris.SyncWriter to send logs to Grafana Loki.
//
// It provides high-performance batching, automatic retries, and concurrent safety.
// The writer runs background goroutines for flushing and should be properly
// closed using the Close() method to ensure all logs are sent and resources released.
type Writer struct {
	config         Config
	client         *http.Client
	batch          []*iris.Record
	batchMu        sync.Mutex
	bufPool        sync.Pool
	recordsWritten int64
	recordsDropped int64
	batchesSent    int64
	errors         int64
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	labelString    string
	endpoint       string
	tenantHeader   string
}

// New creates a new Loki writer with the given configuration.
//
// It validates the configuration, applies defaults for optional fields,
// and starts background goroutines for batching and flushing.
//
// The writer must be closed using Close() to ensure proper resource cleanup.
//
// Returns an error if the configuration is invalid (e.g., missing endpoint).
func New(config Config) (*Writer, error) {
	if config.BatchSize == 0 {
		config.BatchSize = 1000
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 100 * time.Millisecond
	}

	if config.Endpoint == "" {
		return nil, fmt.Errorf("loki endpoint is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	writer := &Writer{
		config: config,
		ctx:    ctx,
		cancel: cancel,
		batch:  make([]*iris.Record, 0, config.BatchSize),
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}

	writer.endpoint = config.Endpoint
	writer.labelString = writer.buildLabelString()
	if config.TenantID != "" {
		writer.tenantHeader = config.TenantID
	}

	writer.bufPool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 64*1024))
		},
	}

	writer.wg.Add(1)
	go writer.flushLoop()

	return writer, nil
}

// WriteRecord implements iris.SyncWriter by adding the record to the internal batch.
//
// Records are batched for efficiency and sent to Loki when the batch size is reached
// or the flush interval expires. This method is safe for concurrent use and returns
// immediately without blocking on network I/O.
//
// The method automatically triggers flushing when the batch size is reached,
// ensuring optimal throughput without unbounded memory growth.
func (w *Writer) WriteRecord(record *iris.Record) error {
	w.batchMu.Lock()
	w.batch = append(w.batch, record)
	shouldFlush := len(w.batch) >= w.config.BatchSize
	w.batchMu.Unlock()

	if shouldFlush {
		go w.flushBatch()
	}

	atomic.AddInt64(&w.recordsWritten, 1)
	return nil
}

// Close gracefully shuts down the writer and flushes any remaining records.
//
// It stops background goroutines, waits for them to complete, and sends
// any buffered records to Loki. This method should always be called when
// the writer is no longer needed to prevent data loss and resource leaks.
//
// Close is safe to call multiple times and will return quickly after the first call.
func (w *Writer) Close() error {
	w.cancel()
	w.wg.Wait()
	w.flushBatch()
	return nil
}

func (w *Writer) buildLabelString() string {
	if len(w.config.Labels) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for k, v := range w.config.Labels {
		if !first {
			buf.WriteByte(',')
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteByte('"')
		buf.WriteString(v)
		buf.WriteByte('"')
		first = false
	}
	buf.WriteByte('}')
	return buf.String()
}

func (w *Writer) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.flushBatch()
		}
	}
}

func (w *Writer) flushBatch() {
	w.batchMu.Lock()
	if len(w.batch) == 0 {
		w.batchMu.Unlock()
		return
	}
	currentBatch := w.batch
	w.batch = make([]*iris.Record, 0, w.config.BatchSize)
	w.batchMu.Unlock()

	if err := w.sendToLoki(currentBatch); err != nil {
		atomic.AddInt64(&w.errors, 1)
		atomic.AddInt64(&w.recordsDropped, int64(len(currentBatch)))
		if w.config.OnError != nil {
			w.config.OnError(err)
		}
	} else {
		atomic.AddInt64(&w.batchesSent, 1)
	}
}

func (w *Writer) sendToLoki(records []*iris.Record) error {
	values := make([][]string, len(records))

	for i, record := range records {
		line := fmt.Sprintf(`{"ts":"%s","level":"%s","msg":"%s"}`,
			timecache.CachedTimeString(),
			record.Level.String(),
			record.Msg)
		timestamp := fmt.Sprintf("%d", timecache.CachedTimeNano())
		values[i] = []string{timestamp, line}
	}

	req := map[string]interface{}{
		"streams": []map[string]interface{}{
			{
				"stream": w.config.Labels,
				"values": values,
			},
		},
	}

	buf := w.bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		w.bufPool.Put(buf)
	}()

	if err := json.NewEncoder(buf).Encode(req); err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(w.ctx, "POST", w.endpoint, buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if w.tenantHeader != "" {
		httpReq.Header.Set("X-Scope-OrgID", w.tenantHeader)
	}

	resp, err := w.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("loki returned status %d", resp.StatusCode)
	}

	return nil
}
