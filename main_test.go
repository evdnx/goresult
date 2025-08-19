package goresult

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evdnx/golog"
)

// Helper function to assert that a function panics.
func assertPanic(t *testing.T, msg string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic but did not panic", msg)
		}
	}()
	f()
}

func TestOkErr_UnwrapAndUnwrapErr(t *testing.T) {
	// Test Ok
	rOk := Ok(42)
	if !rOk.IsOk() {
		t.Errorf("expected Ok result, got error: %v", rOk)
	}
	if v := rOk.Unwrap(); v != 42 {
		t.Errorf("expected value 42, got %v", v)
	}

	// Test Err
	rErr := Err[int](errors.New("failure"))
	if !rErr.IsErr() {
		t.Errorf("expected Err result")
	}
	// UnwrapErr should succeed.
	errVal := rErr.UnwrapErr()
	if !strings.Contains(errVal.Error(), "failure") {
		t.Errorf("expected error message to contain 'failure', got %v", errVal)
	}

	// Unwrap on an Err result should panic.
	assertPanic(t, "Unwrap on Err", func() {
		_ = rErr.Unwrap()
	})

	// UnwrapErr on an Ok result should panic.
	assertPanic(t, "UnwrapErr on Ok", func() {
		_ = rOk.UnwrapErr()
	})
}

func TestUnwrapOr(t *testing.T) {
	// When Ok, UnwrapOr should return the original value.
	rOk := Ok("hello")
	if v := rOk.UnwrapOr("default"); v != "hello" {
		t.Errorf("expected 'hello', got %v", v)
	}

	// When Err, UnwrapOr should return the default.
	rErr := Err[string](errors.New("oops"))
	if v := rErr.UnwrapOr("default"); v != "default" {
		t.Errorf("expected default value 'default', got %v", v)
	}
}

func TestOrAndOrElse(t *testing.T) {
	// Or: if the result is Ok, Or returns self; if Err, returns the alternative.
	rOk := Ok(100)
	rAlt := Ok(200)
	if res := rOk.Or(rAlt); res.Unwrap() != 100 {
		t.Errorf("expected original Ok value 100, got %v", res.Unwrap())
	}
	rErr := Err[int](errors.New("error"))
	if res := rErr.Or(rAlt); res.Unwrap() != 200 {
		t.Errorf("expected alternative value 200, got %v", res.Unwrap())
	}

	// OrElse: if Err, should compute the alternative.
	altFn := func() Result[int] {
		return Ok(300)
	}
	if res := rOk.OrElse(altFn); res.Unwrap() != 100 {
		t.Errorf("OrElse should not change an Ok result")
	}
	if res := rErr.OrElse(altFn); res.Unwrap() != 300 {
		t.Errorf("OrElse should compute alternative on Err")
	}
}

func TestMapAndAndThen(t *testing.T) {
	// Test Map free function.
	r := Ok(5)
	double := func(x int) int { return x * 2 }
	mapped := Map(r, double)
	if mapped.Unwrap() != 10 {
		t.Errorf("expected 10, got %d", mapped.Unwrap())
	}

	// Map should propagate errors.
	errR := Err[int](errors.New("error"))
	mappedErr := Map(errR, double)
	if !mappedErr.IsErr() {
		t.Errorf("expected error result, got Ok")
	}

	// Test AndThen free function.
	toString := func(x int) Result[string] {
		return Ok(fmt.Sprintf("Value: %d", x))
	}
	chained := AndThen(r, toString)
	if chained.Unwrap() != "Value: 5" {
		t.Errorf("expected 'Value: 5', got %s", chained.Unwrap())
	}

	// AndThen should propagate errors.
	chainedErr := AndThen(errR, toString)
	if !chainedErr.IsErr() {
		t.Errorf("expected error propagation in AndThen")
	}
}

func TestFilter(t *testing.T) {
	isEven := func(x int) bool { return x%2 == 0 }
	// Filtering an Ok value that passes.
	rEven := Ok(4)
	filtered := rEven.Filter(isEven, errors.New("not even"))
	if !filtered.IsOk() || filtered.Unwrap() != 4 {
		t.Errorf("expected value 4 after filtering, got %v", filtered)
	}

	// Filtering an Ok value that fails.
	rOdd := Ok(3)
	filteredOdd := rOdd.Filter(isEven, errors.New("not even"))
	if filteredOdd.IsOk() {
		t.Errorf("expected error result for odd number, got Ok")
	}

	// Filtering an Err value should leave it unchanged.
	rErr := Err[int](errors.New("original error"))
	filteredErr := rErr.Filter(isEven, errors.New("not even"))
	if !filteredErr.IsErr() {
		t.Errorf("expected error result to remain unchanged")
	}
}

func TestRetryWithBackoff(t *testing.T) {
	// Set up a function that fails the first two times and then succeeds.
	attempt := 0
	retryFn := func() Result[string] {
		attempt++
		if attempt < 3 {
			return Err[string](errors.New("temporary failure"))
		}
		return Ok(fmt.Sprintf("succeeded at attempt %d", attempt))
	}

	cfg := BackoffConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Factor:       2,
		Jitter:       0.1,
	}

	result := RetryWithBackoff(5, retryFn, cfg)
	if !result.IsOk() {
		t.Errorf("expected retry to eventually succeed, got error: %v", result.UnwrapErr())
	}
	if !strings.Contains(result.Unwrap(), "attempt 3") {
		t.Errorf("expected success on attempt 3, got %v", result.Unwrap())
	}
}

func TestBatchAndAggregateErrors(t *testing.T) {
	results := []Result[int]{
		Ok(1),
		Err[int](errors.New("error A")),
		Ok(3),
		Err[int](errors.New("error B")),
	}

	oks, errs := BatchResults(results)
	if len(oks) != 2 {
		t.Errorf("expected 2 Ok values, got %d", len(oks))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}

	aggErr := AggregateErrors(errs)
	if !strings.Contains(aggErr.Error(), "error A") || !strings.Contains(aggErr.Error(), "error B") {
		t.Errorf("aggregated error does not contain expected messages: %v", aggErr)
	}
}

func TestOptionalConversion(t *testing.T) {
	// Test ToOption and FromOption with an Ok result.
	r := Ok("hello")
	opt := r.ToOption()
	if !opt.Valid() {
		t.Errorf("expected Option to be valid")
	}
	if opt.Value() != "hello" {
		t.Errorf("expected Option value 'hello', got %v", opt.Value())
	}

	rFromOpt := FromOption(opt, errors.New("no value"))
	if rFromOpt.Unwrap() != "hello" {
		t.Errorf("expected converted Result to have value 'hello'")
	}

	// Test FromOption on an invalid Option.
	invalidOpt := Option[string]{}
	rInvalid := FromOption(invalidOpt, errors.New("missing"))
	if rInvalid.IsOk() {
		t.Errorf("expected Result to be error when Option is invalid")
	}
	if !strings.Contains(rInvalid.UnwrapErr().Error(), "missing") {
		t.Errorf("expected error message to contain 'missing', got %v", rInvalid.UnwrapErr())
	}
}

func TestParallelWithWorkers(t *testing.T) {
	// Create several functions that simulate work.
	funcs := []func() Result[int]{
		func() Result[int] {
			time.Sleep(20 * time.Millisecond)
			return Ok(10)
		},
		func() Result[int] {
			time.Sleep(10 * time.Millisecond)
			return Err[int](errors.New("failed job"))
		},
		func() Result[int] {
			time.Sleep(30 * time.Millisecond)
			return Ok(30)
		},
	}

	results := ParallelWithWorkers(funcs, 2)
	if len(results) != len(funcs) {
		t.Errorf("expected %d results, got %d", len(funcs), len(results))
	}

	// Check individual results.
	if results[0].IsErr() || results[0].Unwrap() != 10 {
		t.Errorf("unexpected result for job 0: %v", results[0])
	}
	if results[1].IsOk() {
		t.Errorf("expected job 1 to fail, got Ok: %v", results[1])
	}
	if results[2].IsErr() || results[2].Unwrap() != 30 {
		t.Errorf("unexpected result for job 2: %v", results[2])
	}
}

func TestDebugStringAndLog(t *testing.T) {
	// Set up a buffer to capture log output
	var buf bytes.Buffer
	logger, err := golog.NewLogger(
		golog.WithWriterProvider(&buf, "console"),
		golog.WithLevel(golog.DebugLevel),
	)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Test DebugString for Ok result.
	rOk := Ok("debug ok")
	debugOk := rOk.DebugString()
	if !strings.Contains(debugOk, "Ok(") {
		t.Errorf("expected DebugString for Ok to contain 'Ok(', got %s", debugOk)
	}

	// Test DebugString for Err result.
	rErr := Err[string](errors.New("debug error"))
	debugErr := rErr.DebugString()
	if !strings.Contains(debugErr, "Err(") {
		t.Errorf("expected DebugString for Err to contain 'Err(', got %s", debugErr)
	}

	// Test Log by capturing output.
	rOk.Log(logger)
	rErr.Log(logger)
	logOutput := buf.String()
	if !strings.Contains(logOutput, `status=Ok`) || !strings.Contains(logOutput, `value=debug ok`) {
		t.Errorf("expected log output to include Ok message with value, got %s", logOutput)
	}
	if !strings.Contains(logOutput, `status=Err`) || !strings.Contains(logOutput, `error=debug error`) {
		t.Errorf("expected log output to include Err message with error, got %s", logOutput)
	}
}
