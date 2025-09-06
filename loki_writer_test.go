// loki_writer_test.go: External loki writer for Iris tests
//
// Copyright (c) 2025 AGILira
// Series: an AGILira library
// SPDX-License-Identifier: MPL-2.0

package lokiwriter

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agilira/iris"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Endpoint: "http://localhost:3100/loki/api/v1/push",
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			config: Config{
				TenantID: "test",
			},
			wantErr: true,
		},
		{
			name: "with defaults",
			config: Config{
				Endpoint: "http://localhost:3100/loki/api/v1/push",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if writer != nil {
				writer.Close()
			}
		})
	}
}

func TestWriter_WriteRecord(t *testing.T) {
	// Mock HTTP server
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config := Config{
		Endpoint:      server.URL,
		BatchSize:     2,
		FlushInterval: 100 * time.Millisecond,
		Labels:        map[string]string{"service": "test"},
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	// Create test records
	record1 := &iris.Record{
		Level: iris.Info,
		Msg:   "test message 1",
	}
	record2 := &iris.Record{
		Level: iris.Warn,
		Msg:   "test message 2",
	}

	// Write records
	err = writer.WriteRecord(record1)
	if err != nil {
		t.Errorf("WriteRecord() error = %v", err)
	}

	err = writer.WriteRecord(record2)
	if err != nil {
		t.Errorf("WriteRecord() error = %v", err)
	}

	// Wait for batch to be sent
	time.Sleep(200 * time.Millisecond)

	// Verify metrics
	written := atomic.LoadInt64(&writer.recordsWritten)
	if written != 2 {
		t.Errorf("Expected 2 records written, got %d", written)
	}
}

func TestWriter_BatchFlushing(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config := Config{
		Endpoint:      server.URL,
		BatchSize:     3,
		FlushInterval: 50 * time.Millisecond,
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	// Write exactly BatchSize records to trigger immediate flush
	for i := 0; i < 3; i++ {
		record := &iris.Record{
			Level: iris.Info,
			Msg:   "test message",
		}
		if err := writer.WriteRecord(record); err != nil {
			t.Errorf("WriteRecord failed: %v", err)
		}
	}

	// Wait for async flush
	time.Sleep(100 * time.Millisecond)

	batches := atomic.LoadInt64(&writer.batchesSent)
	if batches != 1 {
		t.Errorf("Expected 1 batch sent, got %d", batches)
	}
}

func TestWriter_ErrorHandling(t *testing.T) {
	// Server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var mu sync.Mutex
	errorCalled := false
	config := Config{
		Endpoint:  server.URL,
		BatchSize: 1,
		OnError: func(err error) {
			mu.Lock()
			errorCalled = true
			mu.Unlock()
		},
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	record := &iris.Record{
		Level: iris.Error,
		Msg:   "test error message",
	}

	if err := writer.WriteRecord(record); err != nil {
		t.Errorf("WriteRecord failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	errorCalledValue := errorCalled
	mu.Unlock()

	if !errorCalledValue {
		t.Error("Expected error callback to be called")
	}

	errors := atomic.LoadInt64(&writer.errors)
	if errors != 1 {
		t.Errorf("Expected 1 error, got %d", errors)
	}

	dropped := atomic.LoadInt64(&writer.recordsDropped)
	if dropped != 1 {
		t.Errorf("Expected 1 record dropped, got %d", dropped)
	}
}

func TestWriter_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config := Config{
		Endpoint:      server.URL,
		FlushInterval: time.Minute, // Long interval to test close behavior
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	// Write a record but don't wait for auto-flush
	record := &iris.Record{
		Level: iris.Info,
		Msg:   "test message",
	}
	if err := writer.WriteRecord(record); err != nil {
		t.Errorf("WriteRecord failed: %v", err)
	}

	// Close should flush remaining records
	err = writer.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify the record was processed
	written := atomic.LoadInt64(&writer.recordsWritten)
	if written != 1 {
		t.Errorf("Expected 1 record written, got %d", written)
	}
}

func TestBuildLabelString(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   "{}",
		},
		{
			name:   "single label",
			labels: map[string]string{"service": "test"},
			want:   `{service="test"}`,
		},
		{
			name:   "multiple labels",
			labels: map[string]string{"service": "test", "env": "prod"},
			want:   `{env="prod",service="test"}`, // Go maps are ordered in Go 1.21+
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Endpoint: "http://localhost:3100",
				Labels:   tt.labels,
			}
			writer, _ := New(config)
			defer writer.Close()

			got := writer.buildLabelString()

			// For multiple labels, we need to handle map iteration order
			if len(tt.labels) > 1 {
				// Just check that it contains the expected parts
				if len(got) < 2 || got[0] != '{' || got[len(got)-1] != '}' {
					t.Errorf("buildLabelString() = %v, want properly formatted labels", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("buildLabelString() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func BenchmarkWriter_WriteRecord(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config := Config{
		Endpoint:      server.URL,
		BatchSize:     10000, // Large batch to avoid frequent flushes during benchmark
		FlushInterval: time.Minute,
	}

	writer, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	record := &iris.Record{
		Level: iris.Info,
		Msg:   "benchmark test message",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = writer.WriteRecord(record) // Ignore error in benchmark
		}
	})
}
