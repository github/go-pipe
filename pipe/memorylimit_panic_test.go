package pipe

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const memWatchPanicSentinel = "memwatch-panic-sentinel"

// fakeLimitableStage is a minimal LimitableStage whose `GetRSSAnon()`
// method panics, and whose `Wait()` method returns after that panic
// has been issued.
type fakeLimitableStage struct {
	done chan struct{}
}

func (fakeLimitableStage) Name() string                  { return "fake" }
func (fakeLimitableStage) Preferences() StagePreferences { return StagePreferences{} }
func (fakeLimitableStage) Start(
	context.Context, Env, io.ReadCloser, io.WriteCloser, StartOptions,
) error {
	return nil
}
func (stage fakeLimitableStage) Wait() error {
	<-stage.done
	return nil
}

func (stage fakeLimitableStage) GetRSSAnon(context.Context) (uint64, error) {
	close(stage.done)
	panic(memWatchPanicSentinel)
}

func (fakeLimitableStage) Kill(error) {}

func panickingWatchStage() Stage {
	stage := fakeLimitableStage{
		done: make(chan struct{}),
	}
	return MemoryWatch(stage, func(*Event) {}, WithMemoryLimit(1))
}

// TestMemoryWatchStagePanicWithHandlerSurfaced verifies that a panic
// escaping the memory-watch goroutine (where the user-supplied event
// handler runs) is recovered via the configured panic handler and
// surfaced as the stage's Wait error.
func TestMemoryWatchStagePanicWithHandlerSurfaced(t *testing.T) {
	ms := panickingWatchStage()
	opts := StartOptions{
		PanicHandler: func(p any) error { return fmt.Errorf("recovered: %v", p) },
	}

	if err := ms.Start(context.Background(), Env{}, nil, nil, opts); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	err := ms.Wait()
	if err == nil {
		t.Fatal("expected Wait to surface the recovered panic, got nil")
	}
	if !strings.Contains(err.Error(), memWatchPanicSentinel) {
		t.Fatalf("expected error to mention %q, got: %v", memWatchPanicSentinel, err)
	}
}

// TestMemoryWatchStagePanicWithoutHandlerPropagates verifies that the
// memory-watch sampling path does not swallow a panic. The monitor
// goroutine only installs a recover when a handler is present (see
// memoryWatchStage.monitor), so we exercise update() directly and assert
// that it propagates the panic. update() is used rather than watch() so the
// assertion is synchronous and ticker-independent: a regression that stopped
// the panic would fail the test rather than hang on the ticker loop.
func TestMemoryWatchStagePanicWithoutHandlerPropagates(t *testing.T) {
	limit := uint64(1)
	mw := memoryWatchStage{
		stage:        fakeLimitableStage{done: make(chan struct{})},
		eventHandler: func(*Event) {},
		limit:        &limit,
	}

	assert.PanicsWithValue(t, memWatchPanicSentinel, func() {
		mw.update(context.Background())
	})
}

// killTrackingStage is a LimitableStage that reports an over-limit RSS
// and blocks in Wait until it is killed, recording that the kill
// happened. It lets a test assert that the memory limit is enforced.
type killTrackingStage struct {
	killed chan struct{}
	done   chan struct{}
}

func newKillTrackingStage() *killTrackingStage {
	return &killTrackingStage{
		killed: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (*killTrackingStage) Name() string                  { return "kill-tracking" }
func (*killTrackingStage) Preferences() StagePreferences { return StagePreferences{} }
func (*killTrackingStage) Start(
	context.Context, Env, io.ReadCloser, io.WriteCloser, StartOptions,
) error {
	return nil
}
func (s *killTrackingStage) Wait() error { <-s.done; return ErrMemoryLimitExceeded }
func (*killTrackingStage) GetRSSAnon(context.Context) (uint64, error) {
	return 1 << 30, nil
}

func (s *killTrackingStage) Kill(error) {
	select {
	case <-s.killed:
		// already killed
	default:
		close(s.killed)
		close(s.done)
	}
}

// TestMemoryLimitKillsEvenIfEventHandlerPanics verifies that an over-limit
// stage is still killed (the limit enforced) even when the user's event
// handler panics and that panic is recovered by the configured handler.
// Without the kill being guaranteed during unwinding, the runaway stage
// would never be killed and Wait would hang.
func TestMemoryLimitKillsEvenIfEventHandlerPanics(t *testing.T) {
	stage := newKillTrackingStage()
	eventHandler := func(*Event) { panic(memWatchPanicSentinel) }
	ms := MemoryWatch(stage, eventHandler, WithMemoryLimit(1))
	opts := StartOptions{
		PanicHandler: func(p any) error { return fmt.Errorf("recovered: %v", p) },
	}

	if err := ms.Start(context.Background(), Env{}, nil, nil, opts); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	select {
	case <-stage.killed:
		// expected: the limit was enforced despite the handler panic.
	case <-time.After(5 * time.Second):
		t.Fatal("over-limit stage was not killed after the event handler panicked")
	}

	if err := ms.Wait(); err != ErrMemoryLimitExceeded {
		t.Fatalf("Wait = %v, want %v", err, ErrMemoryLimitExceeded)
	}
}
