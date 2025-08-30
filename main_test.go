package goresult

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper assertions
// ---------------------------------------------------------------------------

func assertTrue(t *testing.T, cond bool, msg string) {
	if !cond {
		t.Fatalf("expected true: %s", msg)
	}
}

func assertFalse(t *testing.T, cond bool, msg string) {
	if cond {
		t.Fatalf("expected false: %s", msg)
	}
}

func assertEqual[T comparable](t *testing.T, got, want T, msg string) {
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// ---------------------------------------------------------------------------
// Construction & basic predicates
// ---------------------------------------------------------------------------

func TestOkAndErrBasic(t *testing.T) {
	ok := Ok(42)
	assertTrue(t, ok.IsOk(), "Ok should be ok")
	assertFalse(t, ok.IsErr(), "Ok should not be err")
	assertEqual(t, ok.Unwrap(), 42, "Unwrap on Ok")

	errVal := errors.New("boom")
	er := Err[int](errVal)
	assertTrue(t, er.IsErr(), "Err should be err")
	assertFalse(t, er.IsOk(), "Err should not be ok")
	// Compare the underlying message rather than the concrete error value
	if er.UnwrapErr().Error() != errVal.Error() {
		t.Fatalf("UnwrapErr on Err: got %s, want %s", er.UnwrapErr().Error(), errVal.Error())
	}
}

// ---------------------------------------------------------------------------
// UnwrapOr / UnwrapOrElse
// ---------------------------------------------------------------------------

func TestUnwrapOr(t *testing.T) {
	ok := Ok("hello")
	assertEqual(t, ok.UnwrapOr("fallback"), "hello", "UnwrapOr on Ok")

	er := Err[string](errors.New("bad"))
	assertEqual(t, er.UnwrapOr("fallback"), "fallback", "UnwrapOr on Err")
}

func TestUnwrapOrElse(t *testing.T) {
	calls := 0
	f := func() string {
		calls++
		return "generated"
	}
	ok := Ok("present")
	assertEqual(t, ok.UnwrapOrElse(f), "present", "UnwrapOrElse on Ok")
	if calls != 0 {
		t.Fatalf("fallback should not be called for Ok (calls=%d)", calls)
	}

	er := Err[string](errors.New("bad"))
	assertEqual(t, er.UnwrapOrElse(f), "generated", "UnwrapOrElse on Err")
	if calls != 1 {
		t.Fatalf("fallback should be called exactly once (calls=%d)", calls)
	}
}

// ---------------------------------------------------------------------------
// Or / OrElse
// ---------------------------------------------------------------------------

func TestOrOrElse(t *testing.T) {
	alt := Ok(99)

	ok := Ok(1)
	assertEqual(t, ok.Or(alt).Unwrap(), 1, "Or should keep original Ok")

	er := Err[int](errors.New("oops"))
	assertEqual(t, er.Or(alt).Unwrap(), 99, "Or should replace Err with alt")

	altFn := func() Result[int] { return Ok(77) }
	assertEqual(t, er.OrElse(altFn).Unwrap(), 77, "OrElse should invoke function")
}

// ---------------------------------------------------------------------------
// Map, AndThen, MapErr
// ---------------------------------------------------------------------------

func TestMapAndThen(t *testing.T) {
	double := func(i int) int { return i * 2 }

	ok := Ok(3)
	mapped := Map(ok, double)
	assertTrue(t, mapped.IsOk(), "Map on Ok stays ok")
	assertEqual(t, mapped.Unwrap(), 6, "Map applied correctly")

	er := Err[int](errors.New("bad"))
	mappedErr := Map(er, double)
	assertTrue(t, mappedErr.IsErr(), "Map on Err stays err")

	chain := func(i int) Result[string] {
		return Ok(fmt.Sprintf("val-%d", i))
	}
	chained := AndThen(ok, chain)
	assertTrue(t, chained.IsOk(), "AndThen on Ok stays ok")
	assertEqual(t, chained.Unwrap(), "val-3", "AndThen produced correct value")

	chainedErr := AndThen(er, chain)
	assertTrue(t, chainedErr.IsErr(), "AndThen on Err stays err")

	// MapErr – ensure stack wrapper is added but original message preserved
	base := errors.New("original")
	r := Err[int](base)
	converted := r.MapErr(func(e error) error { return fmt.Errorf("wrapped: %w", e) })
	assertTrue(t, converted.IsErr(), "MapErr keeps err")
	if !contains(converted.UnwrapErr().Error(), "wrapped: original") {
		t.Fatalf("MapErr error message unexpected: %s", converted.UnwrapErr().Error())
	}
}

// ---------------------------------------------------------------------------
// Filter
// ---------------------------------------------------------------------------

func TestFilter(t *testing.T) {
	ok := Ok(10)
	pass := ok.Filter(func(v int) bool { return v > 5 }, errors.New("too small"))
	assertTrue(t, pass.IsOk(), "Filter passes when predicate true")

	fail := ok.Filter(func(v int) bool { return v < 5 }, errors.New("too large"))
	assertTrue(t, fail.IsErr(), "Filter fails when predicate false")
}

// ---------------------------------------------------------------------------
// RetryWithBackoff
// ---------------------------------------------------------------------------

func TestRetryWithBackoffSuccessFirstTry(t *testing.T) {
	attempts := 3
	calls := 0
	fn := func() Result[string] {
		calls++
		return Ok("done")
	}
	cfg := BackoffConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Factor:       2,
		Jitter:       0,
	}
	res := RetryWithBackoff(attempts, fn, cfg)
	assertTrue(t, res.IsOk(), "Should succeed on first try")
	assertEqual(t, res.Unwrap(), "done", "Returned value")
	assertEqual(t, calls, 1, "Only one call")
}

func TestRetryWithBackoffEventualSuccess(t *testing.T) {
	attempts := 5
	var calls int32
	fn := func() Result[int] {
		if atomic.AddInt32(&calls, 1) < 3 {
			return Err[int](errors.New("fail"))
		}
		return Ok(42)
	}
	cfg := BackoffConfig{
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Factor:       2,
		Jitter:       0,
	}
	start := time.Now()
	res := RetryWithBackoff(attempts, fn, cfg)
	elapsed := time.Since(start)

	assertTrue(t, res.IsOk(), "Should eventually succeed")
	assertEqual(t, res.Unwrap(), 42, "Correct value")
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("Expected three attempts, got %d", atomic.LoadInt32(&calls))
	}
	// At least two sleeps (≈3 ms) should have happened.
	if elapsed < 3*time.Millisecond {
		t.Fatalf("Expected backoff delay, got %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// BatchResults & AggregateErrors
// ---------------------------------------------------------------------------

func TestBatchResults(t *testing.T) {
	results := []Result[int]{
		Ok(1),
		Err[int](errors.New("bad")),
		Ok(3),
	}
	oks, errs := BatchResults(results)
	assertEqual(t, len(oks), 2, "Two ok values")
	assertEqual(t, oks[0], 1, "First ok")
	assertEqual(t, oks[1], 3, "Second ok")
	assertEqual(t, len(errs), 1, "One error")
	if !contains(errs[0].Error(), "bad") {
		t.Fatalf("Unexpected error message: %s", errs[0].Error())
	}
}

func TestAggregateErrors(t *testing.T) {
	errs := []error{
		errors.New("first"),
		errors.New("second"),
	}
	agg := AggregateErrors(errs)
	if agg == nil {
		t.Fatal("Aggregated error should not be nil")
	}
	if !contains(agg.Error(), "first") || !contains(agg.Error(), "second") {
		t.Fatalf("Aggregated message missing parts: %s", agg.Error())
	}
}

// ---------------------------------------------------------------------------
// Option conversion
// ---------------------------------------------------------------------------

func TestOptionConversion(t *testing.T) {
	ok := Ok("hi")
	opt := ok.ToOption()
	assertTrue(t, opt.Valid(), "Option from Ok should be valid")
	assertEqual(t, opt.Value(), "hi", "Option value matches")

	er := Err[string](errors.New("nope"))
	opt2 := er.ToOption()
	assertFalse(t, opt2.Valid(), "Option from Err should be invalid")

	// FromOption
	res1 := FromOption(opt, errors.New("fallback"))
	assertTrue(t, res1.IsOk(), "res1.IsOk() must be true")
	assertEqual(t, res1.Unwrap(), "hi", "FromOption kept value")

	res2 := FromOption(opt2, errors.New("fallback"))
	assertTrue(t, res2.IsErr(), "res2.IsErr() must be true")
	if !contains(res2.UnwrapErr().Error(), "fallback") {
		t.Fatalf("FromOption error unexpected: %s", res2.UnwrapErr().Error())
	}
}

// ---------------------------------------------------------------------------
// ParallelWithWorkers (including panic handling)
// ---------------------------------------------------------------------------

func TestParallelWithWorkersSuccess(t *testing.T) {
	fns := []func() Result[int]{
		func() Result[int] { return Ok(1) },
		func() Result[int] { return Ok(2) },
		func() Result[int] { return Ok(3) },
	}
	results := ParallelWithWorkers(fns, 2)
	if len(results) != len(fns) {
		t.Fatalf("expected %d results, got %d", len(fns), len(results))
	}
	sum := 0
	for _, r := range results {
		if r.IsErr() {
			t.Fatalf("unexpected error: %v", r.UnwrapErr())
		}
		sum += r.Unwrap()
	}
	assertEqual(t, sum, 6, "Sum of all results")
}

func TestParallelWithWorkersPanicRecovery(t *testing.T) {
	fns := []func() Result[int]{
		func() Result[int] { return Ok(10) },
		func() Result[int] { panic("boom!") },
		func() Result[int] { return Ok(30) },
	}
	results := ParallelWithWorkers(fns, 3)

	if len(results) != 3 {
		t.Fatalf("expected three results")
	}
	// First and third should be ok
	assertTrue(t, results[0].IsOk(), "First and third should be ok")
	assertTrue(t, results[2].IsOk(), "First and third should be ok")
	// Second should be an Err wrapping the panic
	assertTrue(t, results[1].IsErr(), "Second should be an Err wrapping the panic")
	if !contains(results[1].UnwrapErr().Error(), "panic: boom!") {
		t.Fatalf("panic error not captured correctly: %s", results[1].UnwrapErr().Error())
	}
}

// ---------------------------------------------------------------------------
// DebugString (sanity)
// ---------------------------------------------------------------------------

func TestDebugString(t *testing.T) {
	ok := Ok(5)
	if ok.DebugString() != "Ok(5)" {
		t.Fatalf("unexpected debug string: %s", ok.DebugString())
	}
	er := Err[int](errors.New("bad"))
	if er.DebugString() != "Err(bad)" {
		t.Fatalf("unexpected debug string: %s", er.DebugString())
	}
}

// ---------------------------------------------------------------------------
// tiny helper
// ---------------------------------------------------------------------------

// contains reports whether substr appears within s.
// It returns true for an empty substr (mirroring the behaviour of strings.Contains).
func contains(s, substr string) bool {
	// An empty substring is considered to be present in any string.
	if substr == "" {
		return true
	}
	return strings.Contains(s, substr)
}
