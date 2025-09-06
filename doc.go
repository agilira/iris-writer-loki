// Package lokiwriter provides a high-performance Loki writer for the Iris logging framework.
//
// This package implements the iris.SyncWriter interface to enable shipping logs to Grafana Loki
// with optimal performance characteristics. It's designed as an external module to maintain
// the zero-dependency philosophy of the core Iris library while providing powerful log
// aggregation capabilities.
//
// # Architecture
//
// The lokiwriter integrates with Iris through the SyncWriter interface:
//
//	iris (core) → SyncWriter interface → iris-writer-loki → Grafana Loki
//
// This modular design ensures:
//   - Zero dependencies in core Iris
//   - Independent versioning and updates
//   - Pluggable functionality
//   - Performance optimizations specific to Loki
//
// # Performance Characteristics
//
// The writer is optimized for high-throughput scenarios:
//   - Batching: Configurable batch sizes with automatic flushing
//   - Buffering: Internal buffering with sync.Pool for memory efficiency
//   - Concurrency: Safe for concurrent use across goroutines
//   - Timecache: Uses optimized timestamp generation
//   - Non-blocking: Asynchronous flushing to prevent blocking main execution
//
// # Basic Usage
//
//	package main
//
//	import (
//	    "log"
//	    "time"
//
//	    "github.com/agilira/iris"
//	    lokiwriter "github.com/agilira/iris-writer-loki"
//	)
//
//	func main() {
//	    config := lokiwriter.Config{
//	        Endpoint:      "http://localhost:3100/loki/api/v1/push",
//	        TenantID:      "my-tenant",
//	        Labels:        map[string]string{"service": "my-app"},
//	        BatchSize:     1000,
//	        FlushInterval: time.Second,
//	    }
//
//	    writer, err := lokiwriter.New(config)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer writer.Close()
//
//	    logger := iris.New(iris.WithSyncWriter(writer))
//	    logger.Info("Hello from Iris to Loki!")
//	}
//
// # Configuration Options
//
// The Config struct supports extensive customization:
//   - Endpoint: Loki push API endpoint URL (required)
//   - TenantID: Optional tenant ID for multi-tenant setups
//   - Labels: Static labels attached to all log streams
//   - BatchSize: Records per batch (default: 1000)
//   - FlushInterval: Maximum wait time for incomplete batches (default: 1s)
//   - Timeout: HTTP request timeout (default: 10s)
//   - OnError: Optional callback for error handling
//   - MaxRetries: Retry attempts for failed requests (default: 3)
//   - RetryDelay: Delay between retry attempts (default: 100ms)
//
// # Error Handling
//
// The writer provides robust error handling through multiple mechanisms:
//   - OnError callback for custom error processing
//   - Automatic retries with exponential backoff
//   - Graceful degradation with record dropping on persistent failures
//   - Metrics tracking for monitoring (errors, records written/dropped, batches sent)
//
// # Integration Examples
//
// With custom error handling:
//
//	config := lokiwriter.Config{
//	    Endpoint: "http://localhost:3100/loki/api/v1/push",
//	    OnError: func(err error) {
//	        log.Printf("Loki writer error: %v", err)
//	        // Custom error handling logic
//	    },
//	}
//
// With multiple writers using iris.MultiWriter:
//
//	consoleWriter := iris.NewConsoleWriter()
//	lokiWriter, _ := lokiwriter.New(config)
//	multiWriter := iris.NewMultiWriter(consoleWriter, lokiWriter)
//	logger := iris.New(iris.WithSyncWriter(multiWriter))
//
// # Thread Safety
//
// All public methods are safe for concurrent use. The writer uses internal
// synchronization to handle concurrent writes while maintaining optimal performance.
//
// # Resource Management
//
// The writer manages resources automatically:
//   - HTTP client with configurable timeouts
//   - Background goroutines for flushing
//   - Memory pools for efficient buffer reuse
//   - Proper cleanup on Close()
//
// Always call Close() to ensure all buffered records are flushed and resources
// are properly released.
//
// # Monitoring
//
// The writer maintains internal metrics accessible through atomic operations:
//   - Records written: Total number of records processed
//   - Records dropped: Count of records lost due to errors
//   - Batches sent: Successfully transmitted batches
//   - Errors: Total error count
//
// These metrics can be integrated with monitoring systems for operational visibility.
package lokiwriter
