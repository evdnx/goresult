# `goresult` – A Comprehensive Result Type for Go

The **goresult** package provides a powerful, extensible `Result` type for Go, inspired by Rust’s `Result`.  
It streamlines error handling, supports lazy evaluation, offers configurable retries with exponential back‑off, enables batch processing, and includes first‑class concurrency primitives.

---

## Table of Contents

1. [Installation](#installation)  
2. [Features Overview](#features-overview)  
3. [Quick Start](#quick-start)  
4. [Core Types & Functions](#core-types--functions)  
5. [Advanced Usage](#advanced-usage)  
   - [Retry with Backoff](#retry-with-backoff)  
   - [Batch Processing & Aggregated Errors](#batch-processing--aggregated-errors)  
   - [Parallel Execution with Worker Pools](#parallel-execution-with-worker-pools)  
   - [Optional Conversion](#optional-conversion)  
   - [Debugging & Structured Logging](#debugging--structured-logging)  
6. [Configuration Details](#configuration-details)  
7. [Dependencies](#dependencies)  
8. [Contributing](#contributing)  
9. [License](#license)

---

## Installation  
```bash
    go get -u github.com/evdnx/goresult
```

**Note:** Requires Go 1.20+ (generics).

---

## Features Overview

| Category                     | Description                                                                                 |
|------------------------------|---------------------------------------------------------------------------------------------|
| **Result Type**              | `Ok` for success, `Err` for failure, with rich unwrapping helpers.                        |
| **Lazy Evaluation**          | `OrElse`, `Filter` defer expensive work until needed.                                       |
| **Retry Logic**              | `RetryWithBackoff` with exponential back‑off, jitter, and configurable limits.            |
| **Batch Processing**         | `BatchResults` splits a slice of `Result`s into successes and errors; `AggregateErrors` merges them. |
| **Optional Conversion**      | Interop with optional/value types via `ToOption` / `FromOption`.                           |
| **Debugging & Logging**      | Automatic stack‑trace enrichment, `DebugString`, and `Log` integration with `golog`.        |
| **Concurrency**              | `ParallelWithWorkers` runs arbitrary `Result`‑producing functions in a safe worker pool (panic‑recovery included). |
| **Mapping & Chaining**       | `Map`, `MapErr`, `AndThen` enable functional composition.                                   |

---

## Quick Start
```go
    package main

    import (
        "errors"
        "fmt"
        "math/rand"
        "time"

        "github.com/evdnx/goresult"
        "github.com/evdnx/golog"
    )

    func main() {
        // Initialise logger (stdout provider)
        logger, _ := golog.NewLogger(
            golog.WithStdOutProvider("console"),
            golog.WithLevel(golog.DebugLevel),
        )
        defer logger.Sync()

        // -------------------------------------------------
        // 1️⃣ Basic usage – Ok / Err / Unwrap helpers
        // -------------------------------------------------
        r1 := goresult.Ok(42)
        fmt.Println("r1 is Ok:", r1.IsOk())
        fmt.Println("r1 value:", r1.Unwrap())

        r2 := goresult.Err[int](errors.New("something went wrong"))
        fmt.Println("r2 is Err:", r2.IsErr())
        fmt.Println("r2 error:", r2.UnwrapErr())

        // Provide a fallback value when the result is an error.
        r3 := goresult.Err[int](errors.New("failure"))
        fmt.Println("r3 with default:", r3.UnwrapOr(100))

        // -------------------------------------------------
        // 2️⃣ Mapping & chaining
        // -------------------------------------------------
        double := func(x int) int { return x * 2 }
        mapped := goresult.Map(r1, double)
        fmt.Println("mapped (double):", mapped.Unwrap())

        toString := func(x int) goresult.Result[string] {
            if x < 50 {
                return goresult.Ok(fmt.Sprintf("Number %d is small", x))
            }
            return goresult.Ok(fmt.Sprintf("Number %d is large", x))
        }
        chained := goresult.AndThen(mapped, toString)
        fmt.Println("chained:", chained.Unwrap())

        // -------------------------------------------------
        // 3️⃣ Filtering
        // -------------------------------------------------
        isEven := func(x int) bool { return x%2 == 0 }
        filtered := r1.Filter(isEven, errors.New("not even"))
        if filtered.IsOk() {
            fmt.Println("filtered passes:", filtered.Unwrap())
        } else {
            fmt.Println("filtered rejects:", filtered.UnwrapErr())
        }

        // -------------------------------------------------
        // 4️⃣ Retry with back‑off
        // -------------------------------------------------
        attempt := 0
        retryFn := func() goresult.Result[string] {
            attempt++
            if attempt < 3 {
                return goresult.Err[string](errors.New("temporary failure"))
            }
            return goresult.Ok(fmt.Sprintf("success on attempt %d", attempt))
        }
        cfg := goresult.BackoffConfig{
            InitialDelay: 100 * time.Millisecond,
            MaxDelay:     1 * time.Second,
            Factor:       2,
            Jitter:       0.1,
        }
        if res := goresult.RetryWithBackoff(5, retryFn, cfg); res.IsOk() {
            fmt.Println("retry succeeded:", res.Unwrap())
        } else {
            fmt.Println("retry failed:", res.UnwrapErr())
        }

        // -------------------------------------------------
        // 5️⃣ Batch processing & aggregated errors
        // -------------------------------------------------
        tasks := []goresult.Result[int]{
            goresult.Ok(1),
            goresult.Err[int](errors.New("task 2 failed")),
            goresult.Ok(3),
            goresult.Err[int](errors.New("task 4 failed")),
        }
        oks, errs := goresult.BatchResults(tasks)
        fmt.Println("ok values:", oks)
        fmt.Println("errors:", errs)
        fmt.Println("aggregated error:", goresult.AggregateErrors(errs))

        // -------------------------------------------------
        // 6️⃣ Parallel execution with workers
        // -------------------------------------------------
        funcs := []func() goresult.Result[int]{
            func() goresult.Result[int] { time.Sleep(100 * time.Millisecond); return goresult.Ok(10) },
            func() goresult.Result[int] { time.Sleep(50 * time.Millisecond); return goresult.Err[int](errors.New("job failed")) },
            func() goresult.Result[int] { time.Sleep(150 * time.Millisecond); return goresult.Ok(30) },
        }
        results := goresult.ParallelWithWorkers(funcs, 2)
        for i, r := range results {
            if r.IsOk() {
                fmt.Printf("job %d succeeded: %d\n", i, r.Unwrap())
            } else {
                fmt.Printf("job %d failed: %v\n", i, r.UnwrapErr())
            }
        }

        // -------------------------------------------------
        // 7️⃣ Optional conversion
        // -------------------------------------------------
        opt := r1.ToOption()
        if opt.Valid() {
            fmt.Println("option contains:", opt.Value())
        }
        fmt.Println("back to Result:", goresult.FromOption(opt, errors.New("empty")).Unwrap())

        // -------------------------------------------------
        // 8️⃣ Debugging & logging
        // -------------------------------------------------
        fmt.Println("r1 debug string:", r1.DebugString())
        fmt.Println("r2 debug string:", r2.DebugString())
        r1.Log(logger)
        r2.Log(logger)
    }
```

---

## Core Types & Functions

| Type / Function | Purpose |
|-----------------|---------|
| `Result[T]` | Generic container holding either a value (`Ok`) or an error (`Err`). |
| `Ok[T](v T)` | Construct a successful result. |
| `Err[T](e error)` | Construct a failed result. |
| `IsOk()`, `IsErr()` | Predicate checks. |
| `Unwrap()`, `UnwrapOr(default)` | Retrieve the value (panics on `Err`). |
| `UnwrapErr()` | Retrieve the stored error. |
| `Or(other)`, `OrElse(fn)` | Provide an alternate result. |
| `Filter(pred, err)` | Keep `Ok` only if predicate holds. |
| `Map(fn)`, `MapErr(fn)` | Transform value or error. |
| `AndThen(fn)` | Chain computations returning `Result`. |
| `RetryWithBackoff(attempts, fn, cfg)` | Retry a function with exponential back‑off. |
| `BatchResults([]Result[T])` | Split slice into successes & errors. |
| `AggregateErrors([]error)` | Combine multiple errors into one. |
| `ParallelWithWorkers([]func() Result[T], workers)` | Run functions concurrently with panic recovery. |
| `ToOption()`, `FromOption(opt, fallback)` | Convert between `Result` and optional values. |
| `DebugString()`, `Log(logger)` | Human‑readable representation & structured logging. |

---

## Advanced Usage

### Retry with Backoff
```go
    cfg := goresult.BackoffConfig{
        InitialDelay: 50 * time.Millisecond,
        MaxDelay:     2 * time.Second,
        Factor:       1.5,
        Jitter:       0.2,
    }
    res := goresult.RetryWithBackoff(5, myFunc, cfg)
```

* `InitialDelay` – base wait before first retry.  
* `MaxDelay` – ceiling for exponential growth.  
* `Factor` – multiplier applied each attempt.  
* `Jitter` – random variation (0‑1) to avoid thundering herd.

### Batch Processing & Aggregated Errors
```go
    results := []goresult.Result[int]{ ... }
    oks, errs := goresult.BatchResults(results)
    if agg := goresult.AggregateErrors(errs); agg != nil {
        fmt.Println("combined error:", agg)
    }
```

### Parallel Execution with Worker Pools
```go
    jobs := []func() goresult.Result[string]{ ... }
    out := goresult.ParallelWithWorkers(jobs, 4) // 4 concurrent workers
```

* Panics inside a job are recovered and turned into an `Err` containing `"panic: <msg>"`.

### Optional Conversion
```go
    opt := goresult.Ok(10).ToOption()
    res := goresult.FromOption(opt, errors.New("missing"))
```

### Debugging & Structured Logging
```go
    logger, _ := golog.NewLogger(golog.WithStdOutProvider("console"))
    result.Log(logger) // logs value or error with stack trace
```

---

## Configuration Details

`BackoffConfig` fields:

| Field          | Type                | Description |
|----------------|---------------------|-------------|
| `InitialDelay` | `time.Duration`     | Base delay before first retry. |
| `MaxDelay`     | `time.Duration`     | Upper bound for delay growth. |
| `Factor`       | `float64`           | Exponential multiplier. |
| `Jitter`       | `float64` (0‑1)     | Random fraction of delay added/subtracted. |

---

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/evdnx/golog` | Structured, high‑performance logging with multiple output providers. |
| Standard library (`errors`, `sync/atomic`, `time`, …) | Core functionality. |

---

## Contributing

1. Fork the repository.  
2. Create a feature branch (`git checkout -b feat/my-feature`).  
3. Write tests covering your changes.  
4. Run `go test ./...` – all tests must pass.  
5. Open a Pull Request describing the change and its motivation.

Please adhere to Go formatting (`gofmt -s`) and keep the public API stable whenever possible.

---

## License

This project is released under the **MIT‑0** license (public domain equivalent). See the `LICENSE` file for details.
