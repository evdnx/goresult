package result

import (
	"fmt"
	"log"
	"math/rand"
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
	return Result[T]{value: value}
}

// Err creates a failed Result with an error.
func Err[T any](err error) Result[T] {
	var zero T
	return Result[T]{value: zero, err: wrapWithStack(err)}
}

// --- Basic Operations ---

func (r Result[T]) IsOk() bool {
	return r.err == nil
}

func (r Result[T]) IsErr() bool {
	return r.err != nil
}

// Unwrap returns the Ok value or panics if the Result is Err.
func (r Result[T]) Unwrap() T {
	if r.IsErr() {
		panic(fmt.Sprintf("called Unwrap on an Err result: %v", r.err))
	}
	return r.value
}

// UnwrapErr returns the Err error or panics if the Result is Ok.
func (r Result[T]) UnwrapErr() error {
	if r.IsOk() {
		panic(fmt.Sprintf("called UnwrapErr on an Ok result: %v", r.value))
	}
	return r.err
}

// UnwrapOr returns the Ok value or a provided default if Err.
func (r Result[T]) UnwrapOr(defaultValue T) T {
	if r.IsErr() {
		return defaultValue
	}
	return r.value
}

// Or returns the Result if Ok, otherwise returns the alternative.
func (r Result[T]) Or(alt Result[T]) Result[T] {
	if r.IsErr() {
		return alt
	}
	return r
}

// OrElse returns the Result if Ok, otherwise calls the provided function to get an alternative.
func (r Result[T]) OrElse(f func() Result[T]) Result[T] {
	if r.IsErr() {
		return f()
	}
	return r
}

// Map transforms an Ok value with the provided function, leaving an Err untouched.
// This is a free function because methods cannot have their own type parameters.
func Map[T any, U any](r Result[T], f func(T) U) Result[U] {
	if r.IsOk() {
		return Ok(f(r.value))
	}
	return Result[U]{err: r.err}
}

// AndThen chains the Result with a function that returns a new Result.
// This is a free function because methods cannot have their own type parameters.
func AndThen[T any, U any](r Result[T], f func(T) Result[U]) Result[U] {
	if r.IsOk() {
		return f(r.value)
	}
	return Result[U]{err: r.err}
}

// MapErr transforms the error in an Err Result, leaving an Ok untouched.
// Since no new type parameter is introduced, it remains a method.
func (r Result[T]) MapErr(f func(error) error) Result[T] {
	if r.IsErr() {
		return Result[T]{value: r.value, err: f(r.err)}
	}
	return r
}

// --- Filtering ---

// Filter applies a predicate to the Ok value; returns Err (with the provided error) if the predicate fails.
func (r Result[T]) Filter(predicate func(T) bool, err error) Result[T] {
	if r.IsOk() && !predicate(r.value) {
		return Err[T](err)
	}
	return r
}

// --- Retry with Backoff ---

// BackoffConfig contains the configuration for the retry backoff.
type BackoffConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
	Jitter       float64
}

// RetryWithBackoff retries a function with exponential backoff.
func RetryWithBackoff[T any](attempts int, fn func() Result[T], cfg BackoffConfig) Result[T] {
	delay := cfg.InitialDelay
	var lastErr error
	for i := 0; i < attempts; i++ {
		res := fn()
		if res.IsOk() {
			return res
		}
		lastErr = res.err
		if i < attempts-1 {
			time.Sleep(addJitter(delay, cfg.Jitter))
			delay = time.Duration(float64(delay) * cfg.Factor)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}
	}
	return Err[T](fmt.Errorf("all retries failed: %w", lastErr))
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
func AggregateErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}
	return fmt.Errorf("aggregate error: %s", strings.Join(messages, "; "))
}

// --- Optional Conversion ---

// Option is a simple optional type.
type Option[T any] struct {
	value T
	valid bool
}

// ToOption converts a Result to an Option (Ok becomes valid, Err becomes invalid).
func (r Result[T]) ToOption() Option[T] {
	if r.IsOk() {
		return Option[T]{value: r.value, valid: true}
	}
	return Option[T]{valid: false}
}

// FromOption converts an Option to a Result, using the provided error if the Option is invalid.
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
	var wg sync.WaitGroup
	taskChan := make(chan int, len(fns))

	// Spawn worker goroutines.
	for i := 0; i < workers; i++ {
		go func() {
			for idx := range taskChan {
				results[idx] = fns[idx]()
				wg.Done()
			}
		}()
	}

	// Enqueue tasks.
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

// Log writes the Result to a provided logger.
func (r Result[T]) Log(logger *log.Logger) {
	if r.IsOk() {
		logger.Printf("Ok(%v)", r.value)
	} else {
		logger.Printf("Err(%v)", r.err)
	}
}

// --- Stack Trace Utilities ---

// stackError wraps an error with a stack trace.
type stackError struct {
	err   error
	stack string
}

func (e *stackError) Error() string {
	return fmt.Sprintf("%s\nStack trace:\n%s", e.err.Error(), e.stack)
}

// wrapWithStack returns an error that includes a stack trace.
// If the error already has a stack trace, it is returned as is.
func wrapWithStack(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*stackError); ok {
		return err
	}
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	pcs = pcs[:n]
	stackTrace := strings.Join(formatStack(pcs, 10), "\n")
	return &stackError{err: err, stack: stackTrace}
}

func formatStack(pcs []uintptr, maxFrames int) []string {
	frames := runtime.CallersFrames(pcs)
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
	if maxFrames > 0 && len(pcs) > maxFrames {
		formatted = append(formatted, fmt.Sprintf("... and %d more", len(pcs)-maxFrames))
	}
	return formatted
}
