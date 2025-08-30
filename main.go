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

// -----------------------------------------------------------------------------
// Result – a generic container for a value or an error.
// -----------------------------------------------------------------------------

// Result holds either a successful value of type T or an error.
// The zero value of Result is an Err with a nil error, which is rarely useful
// – use the constructors below.
type Result[T any] struct {
	value T
	err   error
}

// Ok creates a successful Result containing the supplied value.
func Ok[T any](value T) Result[T] { return Result[T]{value: value} }

// Err creates a failed Result wrapping the supplied error with a stack trace.
func Err[T any](err error) Result[T] {
	var zero T
	return Result[T]{value: zero, err: wrapWithStack(err)}
}

// -----------------------------------------------------------------------------
// Basic predicates
// -----------------------------------------------------------------------------

func (r Result[T]) IsOk() bool  { return r.err == nil }
func (r Result[T]) IsErr() bool { return r.err != nil }

// -----------------------------------------------------------------------------
// Unwrapping helpers
// -----------------------------------------------------------------------------

// Unwrap returns the inner value or panics if the Result is Err.
func (r Result[T]) Unwrap() T {
	if r.IsErr() {
		panic(fmt.Sprintf("called Unwrap on an Err result: %v", r.err))
	}
	return r.value
}

// UnwrapErr returns the inner error or panics if the Result is Ok.
func (r Result[T]) UnwrapErr() error {
	if r.IsOk() {
		panic(fmt.Sprintf("called UnwrapErr on an Ok result: %v", r.value))
	}
	return r.err
}

// UnwrapOr returns the value if Ok, otherwise the supplied default.
func (r Result[T]) UnwrapOr(defaultValue T) T {
	if r.IsErr() {
		return defaultValue
	}
	return r.value
}

// UnwrapOrElse returns the value if Ok, otherwise computes a fallback via f.
func (r Result[T]) UnwrapOrElse(f func() T) T {
	if r.IsErr() {
		return f()
	}
	return r.value
}

// -----------------------------------------------------------------------------
// Alternative fallbacks
// -----------------------------------------------------------------------------

// Or returns r if it is Ok, otherwise returns alt.
func (r Result[T]) Or(alt Result[T]) Result[T] {
	if r.IsErr() {
		return alt
	}
	return r
}

// OrElse returns r if it is Ok, otherwise evaluates f to obtain an alternative.
func (r Result[T]) OrElse(f func() Result[T]) Result[T] {
	if r.IsErr() {
		return f()
	}
	return r
}

// -----------------------------------------------------------------------------
// Transformations
// -----------------------------------------------------------------------------

// Map applies f to the Ok value, preserving any Err unchanged.
func Map[T any, U any](r Result[T], f func(T) U) Result[U] {
	if r.IsOk() {
		return Ok(f(r.value))
	}
	return Result[U]{err: r.err}
}

// AndThen chains r with a function that returns another Result.
func AndThen[T any, U any](r Result[T], f func(T) Result[U]) Result[U] {
	if r.IsOk() {
		return f(r.value)
	}
	return Result[U]{err: r.err}
}

// MapErr transforms the error while preserving the original stack trace.
func (r Result[T]) MapErr(f func(error) error) Result[T] {
	if r.IsErr() {
		return Result[T]{value: r.value, err: wrapWithStack(f(r.err))}
	}
	return r
}

// -----------------------------------------------------------------------------
// Filtering
// -----------------------------------------------------------------------------

// Filter validates the Ok value with predicate; on failure it returns Err(err).
func (r Result[T]) Filter(predicate func(T) bool, err error) Result[T] {
	if r.IsOk() && !predicate(r.value) {
		return Err[T](err)
	}
	return r
}

// -----------------------------------------------------------------------------
// Retry with exponential back‑off
// -----------------------------------------------------------------------------

// BackoffConfig configures the retry strategy.
type BackoffConfig struct {
	InitialDelay time.Duration // first pause before a retry
	MaxDelay     time.Duration // ceiling for the pause
	Factor       float64       // exponential multiplier (>1)
	Jitter       float64       // proportion of jitter (0–1)
}

// RetryWithBackoff repeatedly invokes fn until it succeeds or attempts are exhausted.
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
			time.Sleep(applyJitter(delay, cfg.Jitter))
			delay = time.Duration(float64(delay) * cfg.Factor)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}
	}
	return Err[T](fmt.Errorf("all %d retries failed: %w", attempts, lastErr))
}

// applyJitter adds a random offset (± jitterFactor/2) to the base duration.
// Guarantees a non‑negative result.
func applyJitter(base time.Duration, jitterFactor float64) time.Duration {
	if jitterFactor <= 0 {
		return base
	}
	jitter := time.Duration(float64(base) * jitterFactor * (rand.Float64() - 0.5))
	if jitter < 0 {
		jitter = -jitter
	}
	return base + jitter
}

// -----------------------------------------------------------------------------
// Batch utilities
// -----------------------------------------------------------------------------

// BatchResults separates successful values from errors.
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

// AggregateErrors merges several errors into a single one.
func AggregateErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("multiple errors occurred: %s", strings.Join(msgs, "; "))
	}
}

// -----------------------------------------------------------------------------
// Option conversion
// -----------------------------------------------------------------------------

// Option mirrors the semantics of stdlib’s sql.Null* types.
type Option[T any] struct {
	value T
	valid bool
}

// Valid reports whether the Option holds a value.
func (o Option[T]) Valid() bool { return o.valid }

// Value returns the stored value; panics if invalid.
func (o Option[T]) Value() T {
	if !o.valid {
		panic("called Value on an invalid Option")
	}
	return o.value
}

// ToOption converts a Result into an Option.
func (r Result[T]) ToOption() Option[T] {
	if r.IsOk() {
		return Option[T]{value: r.value, valid: true}
	}
	return Option[T]{}
}

// FromOption builds a Result from an Option, using err when the Option is invalid.
func FromOption[T any](opt Option[T], err error) Result[T] {
	if opt.valid {
		return Ok(opt.value)
	}
	return Err[T](err)
}

// -----------------------------------------------------------------------------
// Parallel execution with a bounded worker pool
// -----------------------------------------------------------------------------

// ParallelWithWorkers runs each function in fns concurrently, limited to workers goroutines.
// It recovers from panics inside the workers and records them as errors.
func ParallelWithWorkers[T any](fns []func() Result[T], workers int) []Result[T] {
	if workers < 1 {
		workers = 1
	}
	results := make([]Result[T], len(fns))
	var wg sync.WaitGroup
	taskCh := make(chan int, len(fns))

	// Launch workers.
	for i := 0; i < workers; i++ {
		go func() {
			for idx := range taskCh {
				func() {
					defer func() {
						if r := recover(); r != nil {
							results[idx] = Err[T](fmt.Errorf("panic: %v", r))
						}
						wg.Done()
					}()
					results[idx] = fns[idx]()
				}()
			}
		}()
	}

	// Feed tasks.
	wg.Add(len(fns))
	for i := range fns {
		taskCh <- i
	}
	close(taskCh)
	wg.Wait()
	return results
}

// -----------------------------------------------------------------------------
// Debug / logging helpers
// -----------------------------------------------------------------------------

// DebugString returns a short human‑readable representation.
func (r Result[T]) DebugString() string {
	if r.IsOk() {
		return fmt.Sprintf("Ok(%v)", r.value)
	}
	return fmt.Sprintf("Err(%v)", r.err)
}

// Log emits a structured log entry using the supplied golog.Logger.
func (r Result[T]) Log(logger *golog.Logger) {
	if r.IsOk() {
		logger.Info("Result", golog.String("status", "Ok"), golog.Any("value", r.value))
	} else {
		logger.Error("Result", golog.String("status", "Err"), golog.Err(r.err))
	}
}

// -----------------------------------------------------------------------------
// Stack‑trace utilities (internal)
// -----------------------------------------------------------------------------

type stackError struct {
	err   error
	stack string
}

func (e *stackError) Error() string { return e.err.Error() }
func (e *stackError) Unwrap() error { return e.err }
func (e *stackError) StackTrace() string {
	return e.stack
}

// wrapWithStack decorates err with a stack trace if it lacks one.
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
	return &stackError{
		err:   err,
		stack: strings.Join(formatStack(pcs, 10), "\n"),
	}
}

// formatStack turns program counters into a slice of readable frames.
func formatStack(pcs []uintptr, maxFrames int) []string {
	frames := runtime.CallersFrames(pcs)
	var out []string
	count := 0
	for {
		f, more := frames.Next()
		out = append(out, fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Function))
		count++
		if !more || (maxFrames > 0 && count >= maxFrames) {
			break
		}
	}
	if maxFrames > 0 && len(pcs) > maxFrames {
		out = append(out, fmt.Sprintf("... and %d more frames", len(pcs)-maxFrames))
	}
	return out
}
