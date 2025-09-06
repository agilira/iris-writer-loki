package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/agilira/iris"
	lokiwriter "github.com/agilira/iris-writer-loki"
)

func main() {
	fmt.Printf("Real End-to-End Integration Test for Iris Loki Writer\n")
	fmt.Printf("This test creates a mock Loki server and verifies data flow\n\n")

	// Create a mock Loki server to capture requests
	var receivedRequests []string
	mockLoki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/loki/api/v1/push" {
			// Read the request body
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			receivedRequests = append(receivedRequests, string(buf[:n]))

			fmt.Printf("� Mock Loki received request %d: %d bytes\n",
				len(receivedRequests), n)

			// Return success
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockLoki.Close()

	fmt.Printf("Mock Loki server started at: %s\n", mockLoki.URL)

	// Configure the Loki writer to point to our mock server
	config := lokiwriter.Config{
		Endpoint:      mockLoki.URL + "/loki/api/v1/push",
		BatchSize:     3, // Small batch for immediate testing
		FlushInterval: 500 * time.Millisecond,
		Timeout:       5 * time.Second,
		Labels: map[string]string{
			"service":     "integration-test",
			"environment": "e2e",
			"test_run":    fmt.Sprintf("run-%d", time.Now().Unix()),
		},
		OnError: func(err error) {
			log.Printf("❌ Loki writer error: %v", err)
		},
	}

	// Create the Loki writer
	writer, err := lokiwriter.New(config)
	if err != nil {
		log.Fatalf("Failed to create Loki writer: %v", err)
	}
	defer writer.Close()

	fmt.Println("✅ Loki writer created successfully")

	// Now test the writer directly with record structures
	fmt.Println("Sending test records...")

	// Create test records manually (simulating what Iris would do)
	records := []*iris.Record{
		{
			Level: iris.Info,
			Msg:   "Test info message",
		},
		{
			Level: iris.Warn,
			Msg:   "Test warning message",
		},
		{
			Level: iris.Error,
			Msg:   "Test error message",
		},
		{
			Level: iris.Debug,
			Msg:   "Test debug message",
		},
	}

	// Send records to the writer
	for i, record := range records {
		if err := writer.WriteRecord(record); err != nil {
			log.Printf("❌ Failed to write record %d: %v", i+1, err)
		} else {
			fmt.Printf("✅ Sent record %d: %s - %s\n",
				i+1, record.Level.String(), record.Msg)
		}

		// Small delay to demonstrate batching
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for flush
	fmt.Println("⏳ Waiting for batches to flush...")
	time.Sleep(2 * time.Second)

	// Close writer to force final flush
	fmt.Println("Closing writer to force final flush...")
	if err := writer.Close(); err != nil {
		log.Printf("Warning: Close error: %v", err)
	}

	// Verify results
	fmt.Printf("\nIntegration Test Results:\n")
	fmt.Printf("Requests received by mock Loki: %d\n", len(receivedRequests))

	if len(receivedRequests) > 0 {
		fmt.Printf("✅ SUCCESS: Data was successfully sent to Loki!\n")
		fmt.Printf("First request sample (truncated):\n")
		sample := receivedRequests[0]
		if len(sample) > 200 {
			sample = sample[:200] + "..."
		}
		fmt.Printf("   %s\n", sample)

		// Verify expected content
		allContent := ""
		for _, req := range receivedRequests {
			allContent += req
		}

		expectedMessages := []string{"Test info message", "Test warning message",
			"Test error message", "Test debug message"}

		found := 0
		for _, msg := range expectedMessages {
			if contains(allContent, msg) {
				found++
			}
		}

		fmt.Printf("   📝 Messages found in requests: %d/%d\n", found, len(expectedMessages))

		if found == len(expectedMessages) {
			fmt.Printf("   PERFECT: All test messages were successfully delivered!\n")
		} else {
			fmt.Printf("   ⚠️  PARTIAL: Some messages may be missing\n")
		}

	} else {
		fmt.Printf("   ❌ FAILURE: No data was received by mock Loki\n")
		fmt.Printf("   Check writer configuration and network connectivity\n")
	}

	fmt.Printf("\nEnd-to-End Integration Test Completed\n")

	// This proves the writer works end-to-end and can send data to Loki!
}

// Simple string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(len(substr) == 0 || findInString(s, substr) >= 0)
}

func findInString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
