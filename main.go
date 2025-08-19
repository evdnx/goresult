package goresult

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/evdnx/golog"
)

// Result represents a value that is either Ok (successful) or Err (failure).
type Result[T any] struct {
	value T
	err   error
}

// --- Constructors ---

// Ok creates a successful Result with the given value.
func Ok[T any](value T) Result[T] {
	return Result[T]{value: value}
}

// Err creates a failed Result with the given error.
func Err[T any](err error) Result[T] {
	var zero T
	return Result[T]{value: zero, err: wrapWithStack(err)}
}

// --- Basic Operations ---

// IsOk returns true if the Result is Ok.
func (r Result[T]) IsOk() bool {
	return r.err == nil
}

// IsErr returns true if the Result is Err.
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

// UnwrapOrElse returns the Ok value or computes a default value using the provided function if Err.
func (r Result[T]) UnwrapOrElse(f func() T) T {
	if r.IsErr() {
		return f()
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
func Map[T any, U any](r Result[T], f func(T) U) Result[U] {
	if r.IsOk() {
		return Ok(f(r.value))
	}
	return Result[U]{err: r.err}
}

// AndThen chains the Result with a function that returns a new Result.
func AndThen[T any, U any](r Result[T], f func(T) Result[U]) Result[U] {
	if r.IsOk() {
		return f(r.value)
	}
	return Result[U]{err: r.err}
}

// MapErr transforms the error in an Err Result, preserving stack traces.
func (r Result[T]) MapErr(f func(error) error) Result[T] {
	if r.IsErr() {
		return Result[T]{value: r.value, err: wrapWithStack(f(r.err))}
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

// BackoffConfig defines parameters for retry backoff.
type BackoffConfig struct {
	InitialDelay time.Duration // Initial delay between retries
	MaxDelay     time.Duration // Maximum delay between retries
	Factor       float64       // Multiplier for exponential backoff
	Jitter       float64       // Jitter factor to add randomness
}

// RetryWithBackoff retries a function with exponential backoff until success or max attempts.
func RetryWithBackoff[T any](attempts int, fn func() Result[T], cfg BackoffConfig) Result[T] {
	if attempts < 1 {
		return Err[T](fmt.Errorf("invalid attempt count: %d", attempts))
	}
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
	return Err[T](fmt.Errorf("all %d retries failed: %w", attempts, lastErr))
}

func addJitter(duration time.Duration, jitterFactor float64) time.Duration {
	jitter := time.Duration(float64(duration) * jitterFactor * (0.5 - rand.Float64()))
	return duration + jitter
}

// --- Batch Operations ---

// BatchResults processes multiple Results and separates Ok values and errors.
func BatchResults[T any](results []Result[T]) (oks []T, errs []error) {
	oks = make([]T, 0, len(results))
	errs = make([]error, 0, len(results))
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
	if len(errs) == 1 {
		return errs[0]
	}
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}
	return fmt.Errorf("multiple errors occurred: %s", strings.Join(messages, "; "))
}

// --- Optional Conversion ---

// Option represents an optional value.
type Option[T any] struct {
	value T
	valid bool
}

// Valid returns true if the Option contains a value.
func (o Option[T]) Valid() bool {
	return o.valid
}

// Value returns the contained value (panics if invalid).
func (o Option[T]) Value() T {
	if !o.valid {
		panic("called Value on an invalid Option")
	}
	return o.value
}

// ToOption converts a Result to an Option (Ok becomes valid, Err becomes invalid).
func (r Result[T]) ToOption() Option[T] {
	if r.IsOk() {
		return Option[T]{value: r.value, valid: true}
	}
	return Option[T]{}
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
	if workers < 1 {
		workers = 1
	}
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

// DebugString provides a concise string representation of the Result for debugging.
func (r Result[T]) DebugString() string {
	if r.IsOk() {
		return fmt.Sprintf("Ok(%v)", r.value)
	}
	return fmt.Sprintf("Err(%v)", r.err)
}

// Log writes a structured log message using the provided golog.Logger.
func (r Result[T]) Log(logger *golog.Logger) {
	if r.IsOk() {
		logger.Info("Result", golog.String("status", "Ok"), golog.Any("value", r.value))
	} else {
		logger.Error("Result", golog.String("status", "Err"), golog.Error(r.err))
	}
}

// --- Stack Trace Utilities ---

// stackError wraps an error with a stack trace.
type stackError struct {
	err   error
	stack string
}

func (e *stackError) Error() string {
	return e.err.Error()
}

// Unwrap returns the underlying error for error unwrapping.
func (e *stackError) Unwrap() error {
	return e.err
}

// StackTrace returns the stack trace as a string.
func (e *stackError) StackTrace() string {
	return e.stack
}

// wrapWithStack adds a stack trace to an error if it doesn't already have one.
func wrapWithStack(err error) error {
	if err == nil {
		return nil
	}
	if se, ok := err.(*stackError); ok {
		return se
	}
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	pcs = pcs[:n]
	return &stackError{
		err:   err,
		stack: strings.Join(formatStack(pcs, 10), "\n"),
	}
}

func formatStack(pcs []uintptr, maxFrames int) []string {
	frames := runtime.CallersFrames(pcs)
	var formatted []string
	count := 0
	for {
		frame, more := frames.Next()
		formatted = append(formatted, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))
		count++
		if !more || (maxFrames > 0 && count >= maxFrames) {
			break
		}
	}
	if maxFrames > 0 && len(pcs) > maxFrames {
		formatted = append(formatted, fmt.Sprintf("... and %d more frames", len(pcs)-maxFrames))
	}
	return formatted
}
