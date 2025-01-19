package result

import (
	"fmt"
	"log"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Result represents a value that can either be Ok (successful) or Err (failure).
type Result[T any] struct {
	value T
	err   error
}

// --- Constructors ---

// Ok creates a successful Result.
func Ok[T any](value T) Result[T] {
	return Result[T]{value: value, err: nil}
}

// Err creates a failed Result with an error.
func Err[T any](err error) Result[T] {
	return Result[T]{value: *new(T), err: wrapWithStack(err)}
}

// --- Basic Operations ---

func (r Result[T]) IsOk() bool {
	return r.err == nil
}

func (r Result[T]) IsErr() bool {
	return r.err != nil
}

func (r Result[T]) Unwrap() T {
	if r.IsErr() {
		panic(fmt.Sprintf("called Unwrap on an Err result: %v", r.err))
	}
	return r.value
}

func (r Result[T]) UnwrapOr(defaultValue T) T {
	if r.IsErr() {
		return defaultValue
	}
	return r.value
}

func (r Result[T]) Or(alt Result[T]) Result[T] {
	if r.IsErr() {
		return alt
	}
	return r
}

func (r Result[T]) OrElse(f func() Result[T]) Result[T] {
	if r.IsErr() {
		return f()
	}
	return r
}

// --- Filtering ---

// Filter applies a predicate to the Ok value; returns Err if the predicate fails.
func (r Result[T]) Filter(predicate func(T) bool, err error) Result[T] {
	if r.IsOk() && !predicate(r.value) {
		return Err[T](err)
	}
	return r
}

// --- Retry with Backoff ---

type BackoffConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
	Jitter       float64
}

// RetryWithBackoff retries a function with backoff.
func RetryWithBackoff[T any](attempts int, fn func() Result[T], cfg BackoffConfig) Result[T] {
	delay := cfg.InitialDelay
	for i := 0; i < attempts; i++ {
		res := fn()
		if res.IsOk() {
			return res
		}
		if i < attempts-1 {
			time.Sleep(addJitter(delay, cfg.Jitter))
			delay = time.Duration(float64(delay) * cfg.Factor)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}
	}
	return Err[T](fmt.Errorf("all retries failed"))
}

func addJitter(duration time.Duration, jitterFactor float64) time.Duration {
	jitter := time.Duration(float64(duration) * jitterFactor * (0.5 - rand.Float64()))
	return duration + jitter
}

// --- Batch Operations ---

// Batch processes multiple Results and separates Ok and Err values.
func Batch[T any](results []Result[T]) ([]T, []error) {
	var oks []T
	var errs []error
	for _, r := range results {
		if r.IsOk() {
			oks = append(oks, r.value)
		} else {
			errs = append(errs, r.err)
		}
	}
	return oks, errs
}

// AggregateErrors combines multiple errors into a single error.
func AggregateErrors(errors []error) error {
	if len(errors) == 0 {
		return nil
	}
	var sb strings.Builder
	for _, err := range errors {
		sb.WriteString(err.Error() + "; ")
	}
	return fmt.Errorf("aggregate error: %s", sb.String())
}

// --- Optional Conversion ---

type Option[T any] struct {
	value T
	valid bool
}

func (r Result[T]) ToOption() Option[T] {
	if r.IsOk() {
		return Option[T]{value: r.value, valid: true}
	}
	return Option[T]{valid: false}
}

func FromOption[T any](opt Option[T], err error) Result[T] {
	if opt.valid {
		return Ok(opt.value)
	}
	return Err[T](err)
}

// --- Parallel Execution with Worker Pool ---

// ParallelWithWorkers executes multiple functions concurrently using a worker pool.
func ParallelWithWorkers[T any](fns []func() Result[T], workers int) []Result[T] {
	results := make([]Result[T], len(fns))
	wg := sync.WaitGroup{}
	taskChan := make(chan int, len(fns))

	// Spawn workers
	for i := 0; i < workers; i++ {
		go func() {
			for idx := range taskChan {
				results[idx] = fns[idx]()
				wg.Done()
			}
		}()
	}

	// Enqueue tasks
	wg.Add(len(fns))
	for idx := range fns {
		taskChan <- idx
	}
	close(taskChan)
	wg.Wait()

	return results
}

// --- Debugging and Logging ---

// DebugString provides a detailed string representation of the Result for debugging.
func (r Result[T]) DebugString() string {
	if r.IsOk() {
		return fmt.Sprintf("Ok(%v)", r.value)
	}
	if err, ok := r.err.(*stackError); ok {
		return fmt.Sprintf("Err(%s)\nStack trace:\n%s", err.err.Error(), err.stack)
	}
	return fmt.Sprintf("Err(%v)", r.err)
}

// Log writes the Result to a logger.
func (r Result[T]) Log(logger *log.Logger) {
	if r.IsOk() {
		logger.Printf("Ok(%v)\n", r.value)
	} else {
		logger.Printf("Err(%v)\n", r.err)
	}
}

// --- Stack Trace Utilities ---

type stackError struct {
	err   error
	stack string
}

func (e *stackError) Error() string {
	return fmt.Sprintf("%s\nStack trace:\n%s", e.err.Error(), e.stack)
}

func wrapWithStack(err error) error {
	if err == nil {
		return nil
	}
	stack := make([]uintptr, 32)
	length := runtime.Callers(3, stack[:])
	stackTrace := strings.Join(formatStack(stack[:length], 10), "\n")
	return &stackError{err: err, stack: stackTrace}
}

func formatStack(stack []uintptr, maxFrames int) []string {
	frames := runtime.CallersFrames(stack)
	var formatted []string
	count := 0
	for {
		frame, more := frames.Next()
		formatted = append(formatted, fmt.Sprintf("%s\n\t%s:%d", frame.Function, frame.File, frame.Line))
		count++
		if !more || (maxFrames > 0 && count >= maxFrames) {
			break
		}
	}
	if maxFrames > 0 && len(stack) > maxFrames {
		formatted = append(formatted, fmt.Sprintf("... and %d more", len(stack)-maxFrames))
	}
	return formatted
}
