# Iris Loki Writer
### an AGILira library

[![CI](https://github.com/agilira/iris-writer-loki/actions/workflows/ci.yml/badge.svg)](https://github.com/agilira/iris-writer-loki/actions/workflows/ci.yml)

External Loki writer module for the Iris.

## Overview

This module implements the `iris.SyncWriter` interface to provide high-performance log shipping to Grafana Loki. It's designed as an external module to keep the core Iris library dependency-free while enabling powerful log aggregation capabilities.

## Features

- **High Performance**: Uses timecache for optimized timestamp generation
- **Batching**: Configurable batch sizes and flush intervals
- **Resilience**: Built-in retry logic and error handling
- **Zero Core Impact**: External module with no dependencies in core Iris
- **Concurrent**: Safe for concurrent use with internal buffering

## Installation

```bash
go get github.com/agilira/iris-writer-loki
```

## Usage

```go
package main

import (
    "log"
    "time"
    
    "github.com/agilira/iris"
    lokiwriter "github.com/agilira/iris-writer-loki"
)

func main() {
    // Configure Loki writer
    config := lokiwriter.Config{
        Endpoint:      "http://localhost:3100/loki/api/v1/push",
        TenantID:      "my-tenant",
        Labels:        map[string]string{"service": "my-app"},
        BatchSize:     1000,
        FlushInterval: time.Second,
        Timeout:       10 * time.Second,
        OnError: func(err error) {
            log.Printf("Loki writer error: %v", err)
        },
    }
    
    writer, err := lokiwriter.New(config)
    if err != nil {
        log.Fatal(err)
    }
    defer writer.Close()
    
    // Use with Iris logger
    logger := iris.New(iris.WithSyncWriter(writer))
    
    logger.Info("Hello from Iris to Loki!")
}
```

## Configuration

- `Endpoint`: Loki push endpoint URL
- `TenantID`: Optional tenant ID for multi-tenant Loki setups
- `Labels`: Static labels to attach to all log streams
- `BatchSize`: Number of records to batch before sending (default: 1000)
- `FlushInterval`: Maximum time to wait before flushing incomplete batches (default: 1s)
- `Timeout`: HTTP request timeout (default: 10s)
- `OnError`: Optional error callback function
- `MaxRetries`: Number of retry attempts (default: 3)
- `RetryDelay`: Delay between retries (default: 100ms)

## Architecture

This module is part of the Iris modular ecosystem:

```
iris (core) → SyncWriter interface → iris-writer-loki → Grafana Loki
```

The external architecture ensures:
- Zero dependencies in core Iris
- Independent versioning and updates
- Modular functionality that can be added/removed as needed
- Performance optimizations specific to Loki integration

iris-writer-loki is licensed under the [Mozilla Public License 2.0](./LICENSE.md).

---

iris-writer-loki • an AGILira library
