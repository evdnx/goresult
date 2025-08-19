# `goresult` - A Comprehensive Result Type for Go

The `goresult` package provides a powerful and extensible **Result type** for Go, inspired by Rust’s `Result`. It simplifies error handling, enables lazy evaluations, supports retries with backoff, facilitates batch operations, and much more.

## Installation

To install the package, run:

```bash
go get -u github.com/evdnx/goresult
```

## Features

- **Result Type**: Use `Ok` for successful results and `Err` for errors.
- **Lazy Evaluation**: Handle errors dynamically with `OrElse` and `Filter`.
- **Retry with Backoff**: Configurable retries with exponential backoff and jitter.
- **Batch Processing**: Aggregate results into successes and errors.
- **Optional Conversion**: Seamlessly convert between `Result` and optional values.
- **Debugging and Logging**: Structured error logging with stack traces.
- **Concurrency**: Support for parallel execution with worker pools.
- **Mapping and Chaining:**
	- Map: (Free function) Transforms a successful Result by applying a function to its value.
	- AndThen: (Free function) Chains a computation that returns a new Result, useful for composing operations.

# Example Usage

Here’s a complete Go program demonstrating the key features of the `result` package:

```go
package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/evdnx/goresult"
)

func main() {
	// Seed the random number generator (used by addJitter)
	rand.Seed(time.Now().UnixNano())

	// === Example 1: Basic Usage (Ok, Err, Unwrap, UnwrapErr, UnwrapOr) ===

	fmt.Println("=== Example 1: Basic Usage ===")
	// Creating an Ok result.
	r1 := result.Ok(42)
	fmt.Println("r1 is Ok:", r1.IsOk())
	fmt.Println("r1 value:", r1.Unwrap())

	// Creating an Err result.
	r2 := result.Err[int](errors.New("something went wrong"))
	fmt.Println("r2 is Err:", r2.IsErr())
	// Trying to unwrap r2 as a value would panic.
	// Instead, we unwrap the error:
	fmt.Println("r2 error:", r2.UnwrapErr())

	// Using UnwrapOr to provide a default value.
	r3 := result.Err[int](errors.New("failure"))
	fmt.Println("r3 value with default:", r3.UnwrapOr(100))

	// === Example 2: Using Map and AndThen (Free Functions) ===

	fmt.Println("\n=== Example 2: Map and AndThen ===")
	// Define a function to double a number.
	double := func(x int) int { return x * 2 }

	// Map r1 (an Ok result) to double its value.
	mapped := result.Map(r1, double)
	fmt.Println("Mapped r1 (doubled):", mapped.Unwrap())

	// AndThen (chain) a computation that returns a new Result.
	toString := func(x int) result.Result[string] {
		if x < 50 {
			return result.Ok(fmt.Sprintf("Number %d is small", x))
		}
		return result.Ok(fmt.Sprintf("Number %d is large", x))
	}
	chained := result.AndThen(mapped, toString)
	fmt.Println("Chained result:", chained.Unwrap())

	// === Example 3: Filtering ===

	fmt.Println("\n=== Example 3: Filter ===")
	// Filter to only allow even numbers.
	isEven := func(x int) bool { return x%2 == 0 }
	filtered := r1.Filter(isEven, errors.New("number is not even"))
	if filtered.IsOk() {
		fmt.Println("Filtered r1 passes:", filtered.Unwrap())
	} else {
		fmt.Println("Filtered r1 rejected:", filtered.UnwrapErr())
	}

	// === Example 4: RetryWithBackoff ===

	fmt.Println("\n=== Example 4: RetryWithBackoff ===")
	// A function that fails a couple of times before succeeding.
	attempt := 0
	retryFn := func() result.Result[string] {
		attempt++
		if attempt < 3 {
			return result.Err[string](errors.New("temporary failure"))
		}
		return result.Ok(fmt.Sprintf("success on attempt %d", attempt))
	}
	// Configure backoff parameters.
	cfg := result.BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Factor:       2,
		Jitter:       0.1,
	}
	retryResult := result.RetryWithBackoff(5, retryFn, cfg)
	if retryResult.IsOk() {
		fmt.Println("Retry succeeded:", retryResult.Unwrap())
	} else {
		fmt.Println("Retry failed:", retryResult.UnwrapErr())
	}

	// === Example 5: Batch and AggregateErrors ===

	fmt.Println("\n=== Example 5: Batch and AggregateErrors ===")
	tasks := []result.Result[int]{
		result.Ok(1),
		result.Err[int](errors.New("error in task 2")),
		result.Ok(3),
		result.Err[int](errors.New("error in task 4")),
	}
	oks, errs := result.Batch(tasks)
	fmt.Println("Batch Ok values:", oks)
	fmt.Println("Batch Errors:", errs)
	aggErr := result.AggregateErrors(errs)
	fmt.Println("Aggregated Error:", aggErr)

	// === Example 6: ParallelWithWorkers ===

	fmt.Println("\n=== Example 6: ParallelWithWorkers ===")
	// Define several functions that return a Result.
	functions := []func() result.Result[int]{
		func() result.Result[int] {
			time.Sleep(100 * time.Millisecond)
			return result.Ok(10)
		},
		func() result.Result[int] {
			time.Sleep(50 * time.Millisecond)
			return result.Err[int](errors.New("failed job"))
		},
		func() result.Result[int] {
			time.Sleep(150 * time.Millisecond)
			return result.Ok(30)
		},
	}
	parallelResults := result.ParallelWithWorkers(functions, 2)
	for i, res := range parallelResults {
		if res.IsOk() {
			fmt.Printf("Job %d succeeded: %d\n", i, res.Unwrap())
		} else {
			fmt.Printf("Job %d failed: %v\n", i, res.UnwrapErr())
		}
	}

	// === Example 7: Optional Conversion ===

	fmt.Println("\n=== Example 7: Optional Conversion ===")
	opt := r1.ToOption()
	if opt.Valid() {
		fmt.Println("Option contains:", opt.Value())
	} else {
		fmt.Println("Option is invalid")
	}
	// Convert Option back to Result.
	rFromOpt := result.FromOption(opt, errors.New("no value"))
	fmt.Println("Converted back to Result:", rFromOpt.Unwrap())

	// === Example 8: DebugString and Log ===

	fmt.Println("\n=== Example 8: DebugString and Log ===")
	logger := log.Default()
	fmt.Println("r1 debug string:", r1.DebugString())
	fmt.Println("r2 debug string:", r2.DebugString())
	r1.Log(logger)
	r2.Log(logger)
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
