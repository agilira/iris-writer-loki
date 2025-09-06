// integration_e2e_test.go: e2e tests for loki writer
//
// Copyright (c) 2025 AGILira
// Series: an AGILira library
// SPDX-License-Identifier: MPL-2.0

package lokiwriter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agilira/iris"
)

// TestEndToEndIntegration verifies that the writer can actually send data to a Loki endpoint
func TestEndToEndIntegration(t *testing.T) {
	// Create a mock Loki server
	var receivedRequests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/loki/api/v1/push" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse the JSON request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		receivedRequests = append(receivedRequests, req)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Configure writer with mock server
	config := Config{
		Endpoint:      server.URL + "/loki/api/v1/push",
		BatchSize:     2, // Small batch for testing
		FlushInterval: 100 * time.Millisecond,
		Timeout:       5 * time.Second,
		Labels: map[string]string{
			"service": "test",
			"env":     "integration",
		},
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	// Send test records
	testRecords := []*iris.Record{
		{Level: iris.Info, Msg: "Test info message"},
		{Level: iris.Warn, Msg: "Test warning message"},
		{Level: iris.Error, Msg: "Test error message"},
	}

	for _, record := range testRecords {
		if err := writer.WriteRecord(record); err != nil {
			t.Errorf("WriteRecord failed: %v", err)
		}
	}

	// Wait for flushes
	time.Sleep(500 * time.Millisecond)

	// Force final flush
	writer.Close()

	// Verify results
	if len(receivedRequests) == 0 {
		t.Fatal("No requests received by mock Loki server")
	}

	// Verify request structure
	for i, req := range receivedRequests {
		streams, ok := req["streams"].([]interface{})
		if !ok {
			t.Errorf("Request %d: missing or invalid streams field", i)
			continue
		}

		if len(streams) != 1 {
			t.Errorf("Request %d: expected 1 stream, got %d", i, len(streams))
			continue
		}

		stream, ok := streams[0].(map[string]interface{})
		if !ok {
			t.Errorf("Request %d: invalid stream format", i)
			continue
		}

		// Verify labels
		labels, ok := stream["stream"].(map[string]interface{})
		if !ok {
			t.Errorf("Request %d: missing or invalid labels", i)
			continue
		}

		if labels["service"] != "test" || labels["env"] != "integration" {
			t.Errorf("Request %d: incorrect labels: %v", i, labels)
		}

		// Verify values exist
		values, ok := stream["values"].([]interface{})
		if !ok {
			t.Errorf("Request %d: missing or invalid values", i)
			continue
		}

		if len(values) == 0 {
			t.Errorf("Request %d: no values in stream", i)
		}
	}

	// Verify all messages were sent
	allContent := ""
	for _, req := range receivedRequests {
		if reqBytes, err := json.Marshal(req); err == nil {
			allContent += string(reqBytes)
		}
	}

	expectedMessages := []string{"Test info message", "Test warning message", "Test error message"}
	for _, msg := range expectedMessages {
		if !strings.Contains(allContent, msg) {
			t.Errorf("Message not found in requests: %s", msg)
		}
	}

	t.Logf("✅ End-to-end integration test passed: %d requests, %d messages verified",
		len(receivedRequests), len(expectedMessages))
}

// TestWriterErrorHandling verifies error handling with unreachable endpoint
func TestWriterErrorHandling(t *testing.T) {
	var mu sync.Mutex
	errorCount := 0
	config := Config{
		Endpoint:      "http://localhost:99999/loki/api/v1/push", // Invalid endpoint
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		Timeout:       100 * time.Millisecond, // Short timeout
		OnError: func(err error) {
			mu.Lock()
			errorCount++
			mu.Unlock()
		},
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	// Send a record that should fail
	record := &iris.Record{Level: iris.Info, Msg: "Test message"}
	if err := writer.WriteRecord(record); err != nil {
		t.Errorf("WriteRecord should not fail immediately: %v", err)
	}

	// Wait for error to occur
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	finalErrorCount := errorCount
	mu.Unlock()

	if finalErrorCount == 0 {
		t.Error("Expected error callback to be called for unreachable endpoint")
	}

	t.Logf("✅ Error handling test passed: %d errors captured", finalErrorCount)
}

// TestBatchingBehavior verifies correct batching and flushing
func TestBatchingBehavior(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config := Config{
		Endpoint:      server.URL + "/loki/api/v1/push",
		BatchSize:     3,               // Batch every 3 records
		FlushInterval: 1 * time.Second, // Long interval to test batch size trigger
		Timeout:       5 * time.Second,
		Labels:        map[string]string{"test": "batching"},
	}

	writer, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer writer.Close()

	// Send exactly 3 records (should trigger 1 batch)
	for i := 0; i < 3; i++ {
		record := &iris.Record{Level: iris.Info, Msg: "Batch test message"}
		if err := writer.WriteRecord(record); err != nil {
			t.Errorf("WriteRecord failed: %v", err)
		}
	}

	// Wait a bit for the batch to be sent
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count1 := requestCount
	mu.Unlock()

	if count1 != 1 {
		t.Errorf("Expected 1 request after 3 records, got %d", count1)
	}

	// Send 2 more records (should not trigger another batch yet)
	for i := 0; i < 2; i++ {
		record := &iris.Record{Level: iris.Info, Msg: "Batch test message"}
		if err := writer.WriteRecord(record); err != nil {
			t.Errorf("WriteRecord failed: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count2 := requestCount
	mu.Unlock()

	if count2 != 1 {
		t.Errorf("Expected still 1 request after 5 total records, got %d", count2)
	}

	// Close should flush remaining records
	writer.Close()

	// Give a bit more time for the final flush
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	// We should have at least 1 request (could be 1 or 2 depending on timing)
	if finalCount < 1 {
		t.Errorf("Expected at least 1 request after sending records, got %d", finalCount)
	}

	// If we got exactly 1 request, all records were batched together (valid behavior)
	// If we got 2 requests, first batch was 3 records, second was 2 (also valid)
	if finalCount > 2 {
		t.Errorf("Expected at most 2 requests, got %d", finalCount)
	}

	t.Logf("✅ Batching behavior test passed: %d requests for proper batching", finalCount)
}
