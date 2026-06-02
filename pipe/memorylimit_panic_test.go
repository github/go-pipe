package pipe

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const memWatchPanicSentinel = "memwatch-panic-sentinel"
const memWatchPanicChildEnv = "GO_PIPE_MEMWATCH_PANIC_CHILD"

// fakeLimitableStage is a minimal LimitableStage whose Wait returns
// immediately, letting a memoryWatchStage test exercise its watch
// goroutine in isolation.
type fakeLimitableStage struct{}

func (fakeLimitableStage) Name() string                  { return "fake" }
func (fakeLimitableStage) Preferences() StagePreferences { return StagePreferences{} }
func (fakeLimitableStage) Start(
	context.Context, Env, io.ReadCloser, io.WriteCloser, StartOptions,
) error {
	return nil
}
func (fakeLimitableStage) Wait() error                                { return nil }
func (fakeLimitableStage) GetRSSAnon(context.Context) (uint64, error) { return 0, nil }
func (fakeLimitableStage) Kill(error)                                 {}

func panickingWatchStage() *memoryWatchStage {
	return &memoryWatchStage{
		stage: fakeLimitableStage{},
		watch: func(context.Context) { panic(memWatchPanicSentinel) },
	}
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

// TestMemoryWatchStagePanicWithoutHandlerPropagates verifies that when
// the memory-watch goroutine panics and no panic handler is installed,
// the panic propagates (crashing the process) rather than being
// silently swallowed. Because that would crash the test binary, the
// scenario runs in a re-exec'd subprocess.
func TestMemoryWatchStagePanicWithoutHandlerPropagates(t *testing.T) {
	if os.Getenv(memWatchPanicChildEnv) == "1" {
		runMemWatchPanicChild()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMemoryWatchStagePanicWithoutHandlerPropagates$", "-test.v") //nolint:gosec // re-exec of this test binary with constant arguments.
	cmd.Env = append(os.Environ(), memWatchPanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err == nil {
		t.Fatalf("expected subprocess to crash from a propagated panic, but it exited 0\noutput:\n%s", output)
	}
	if strings.Contains(output, "SURVIVED") {
		t.Fatalf("panic was swallowed: Wait returned instead of propagating\noutput:\n%s", output)
	}
	if !strings.Contains(output, "panic:") || !strings.Contains(output, memWatchPanicSentinel) {
		t.Fatalf("expected a propagated panic mentioning %q, got:\n%s", memWatchPanicSentinel, output)
	}
}

func runMemWatchPanicChild() {
	ms := panickingWatchStage()

	if err := ms.Start(context.Background(), Env{}, nil, nil, StartOptions{}); err != nil {
		os.Stdout.WriteString("SURVIVED: Start returned err=" + err.Error() + "\n")
		os.Exit(0)
	}

	_ = ms.Wait()

	// Reaching this point at all indicates the panic was swallowed.
	time.Sleep(2 * time.Second)
	os.Stdout.WriteString("SURVIVED: Wait returned\n")
	os.Exit(0)
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
	limit := uint64(1)
	ms := &memoryWatchStage{
		stage: stage,
		watch: (&memoryWatchConfig{limit: &limit}).watchFunc(
			stage, func(*Event) { panic(memWatchPanicSentinel) },
		),
	}
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
