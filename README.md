# `result` - A Comprehensive Result Type for Go

The `result` package provides a powerful and extensible **Result type** for Go, inspired by Rust’s `Result`. It simplifies error handling, enables lazy evaluations, supports retries with backoff, facilitates batch operations, and much more.

## Installation

To install the package, run:

```bash
go get -u github.com/evdnx/go-result
```

## Features

- **Result Type**: Use `Ok` for successful results and `Err` for errors.
- **Lazy Evaluation**: Handle errors dynamically with `OrElse` and `Filter`.
- **Retry with Backoff**: Configurable retries with exponential backoff and jitter.
- **Batch Processing**: Aggregate results into successes and errors.
- **Optional Conversion**: Seamlessly convert between `Result` and optional values.
- **Debugging and Logging**: Structured error logging with stack traces.
- **Concurrency**: Support for parallel execution with worker pools.

# Example Usage

Here’s a complete Go program demonstrating the key features of the `result` package:

```go
package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"github.com/evdnx/go-result/result"
	"time"
)

func unreliableTask() result.Result[string] {
	if rand.Float64() < 0.5 {
		return result.Err[string](fmt.Errorf("random failure"))
	}
	return result.Ok("success")
}

func fallback() result.Result[int] {
	return result.Ok(100)
}

func task1() result.Result[int] { return result.Ok(10) }
func task2() result.Result[int] { return result.Err[int](fmt.Errorf("task 2 failed")) }
func task3() result.Result[int] { return result.Ok(30) }

func main() {
	rand.Seed(time.Now().UnixNano())

	okResult := result.Ok(42)
	fmt.Println(okResult.DebugString()) // Output: Ok(42)

	errResult := result.Err[int](fmt.Errorf("something went wrong"))
	fmt.Println(errResult.DebugString()) // Output: Err(something went wrong)

	res := result.Err[int](fmt.Errorf("initial failure"))
	finalResult := res.OrElse(fallback)
	fmt.Println(finalResult.DebugString()) // Output: Ok(100)

	filtered := result.Ok(42).Filter(func(v int) bool { return v > 50 }, fmt.Errorf("value too small"))
	fmt.Println(filtered.DebugString()) // Output: Err(value too small)

	cfg := result.BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Factor:       2.0,
		Jitter:       0.2,
	}
	retryResult := result.RetryWithBackoff(5, unreliableTask, cfg)
	fmt.Println(retryResult.DebugString()) // Output: Ok(success) or Err(random failure)

	results := []result.Result[int]{
		result.Ok(10),
		result.Err[int](fmt.Errorf("error 1")),
		result.Ok(20),
		result.Err[int](fmt.Errorf("error 2")),
	}
	oks, errs := result.Batch(results)
	fmt.Println("Successes:", oks) // Output: Successes: [10 20]
	fmt.Println("Errors:", errs)   // Output: Errors: [error 1 error 2]

	aggregate := result.AggregateErrors(errs)
	fmt.Println(aggregate) // Output: aggregate error: error 1; error 2;

	opt := result.Ok(42).ToOption()
	fmt.Println(opt) // Output: {42 true}

	backToResult := result.FromOption(opt, fmt.Errorf("no value"))
	fmt.Println(backToResult.DebugString()) // Output: Ok(42)

	parallelResults := result.ParallelWithWorkers([]func() result.Result[int]{task1, task2, task3}, 2)
	for _, res := range parallelResults {
		fmt.Println(res.DebugString())
	}
	// Output:
	// Ok(10)
	// Err(task 2 failed)
	// Ok(30)

	logger := log.New(os.Stdout, "RESULT: ", log.LstdFlags)
	result.Ok("hello world").Log(logger) // Output: RESULT: Ok(hello world)
}
```

# Configuration

Retry logic can be configured using the BackoffConfig struct:

```go
cfg := result.BackoffConfig{
	InitialDelay: 50 * time.Millisecond,
	MaxDelay:     1 * time.Second,
	Factor:       1.5,
	Jitter:       0.1,
}
```

This ensures controlled retries with exponential backoff and jitter to avoid overloading resources.

# Benefits

- **Simplifies error handling** with `Result`-based flow.
- **Improves debugging** with automatic stack traces.
- **Enables concurrency** with built-in support for parallel execution using worker pools.
- **Ensures extensibility** with a modular design that integrates easily into any Go project.

# Contributing

We welcome contributions! Feel free to fork the repository, create a feature branch, and submit a pull request.

# License

This project is licensed under the MIT License.
