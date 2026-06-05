package pipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const memoryPollInterval = time.Second

// ErrMemoryLimitExceeded is the error that will be used to kill a
// process, if necessary, from a MemoryWatch with WithMemoryLimit.
var ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

// LimitableStage is the superset of `Stage` that must be implemented
// by stages passed to MemoryWatch.
type LimitableStage interface {
	Stage

	GetRSSAnon(context.Context) (uint64, error)
	Kill(error)
}

// MemoryWatchOption configures a MemoryWatch stage.
type MemoryWatchOption func(*memoryWatchStage)

// WithMemoryLimit makes MemoryWatch kill the stage when its RSS exceeds
// byteLimit.
func WithMemoryLimit(byteLimit uint64) MemoryWatchOption {
	return func(m *memoryWatchStage) {
		m.limit = &byteLimit
		m.nameSuffix = " with memory limit"
	}
}

// WithPeakUsageLogging makes MemoryWatch log the peak RSS when the stage
// exits.
func WithPeakUsageLogging() MemoryWatchOption {
	return func(m *memoryWatchStage) {
		m.observe = true
	}
}

// MemoryWatch watches the memory usage of the stage and reports via
// eventHandler. With WithMemoryLimit it kills the stage when the limit is
// exceeded; with WithPeakUsageLogging it logs the peak RSS when the stage
// exits. At least one of the two options is required.
//
// If the event handler panics while reporting the over-limit event, the
// stage is still killed. A panic in any other event-handler call (an
// RSS-read error, or the peak-usage report) is recovered via
// StageOptions.PanicHandler and the stage keeps running unmonitored; see
// StageOptions.PanicHandler.
func MemoryWatch(stage Stage, eventHandler func(e *Event), opts ...MemoryWatchOption) Stage {
	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryWatch usage",
			Err:     fmt.Errorf("invalid pipe.MemoryWatch usage"),
		})
		return stage
	}

	m := memoryWatchStage{
		stage:        limitableStage,
		eventHandler: eventHandler,
	}
	for _, opt := range opts {
		opt(&m)
	}

	if m.limit == nil && !m.observe {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryWatch usage",
			Err: fmt.Errorf(
				"pipe.MemoryWatch requires WithMemoryLimit and/or WithPeakUsageLogging",
			),
		})
		return stage
	}

	return &m
}

type memoryWatchStage struct {
	nameSuffix   string
	stage        LimitableStage
	eventHandler func(e *Event)

	limit   *uint64 // non-nil enables kill-at-limit
	observe bool    // log peak RSS when the stage exits

	maxRSS            uint64
	samples           int
	errCount          int
	consecutiveErrors int

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	watchErr error
}

var _ LimitableStage = (*memoryWatchStage)(nil)

func (m *memoryWatchStage) Name() string {
	return m.stage.Name() + m.nameSuffix
}

func (m *memoryWatchStage) Requirements() StageRequirements {
	return m.stage.Requirements()
}

func (m *memoryWatchStage) Start(
	ctx context.Context, opts StageOptions,
	stdin io.Reader, stdinCloser io.Closer,
	stdout io.Writer, stdoutCloser io.Closer,
) error {
	if err := m.stage.Start(ctx, opts, stdin, stdinCloser, stdout, stdoutCloser); err != nil {
		return err
	}

	m.monitor(ctx, opts.PanicHandler)

	return nil
}

func (m *memoryWatchStage) Wait() error {
	err := m.stage.Wait()
	m.stopWatching()
	if err == nil {
		err = m.watchErr // non-nil if panicHandler() returned anything
	}
	return err
}

func (m *memoryWatchStage) GetRSSAnon(ctx context.Context) (uint64, error) {
	return m.stage.GetRSSAnon(ctx)
}

func (m *memoryWatchStage) Kill(err error) {
	m.stage.Kill(err)
	m.stopWatching()
}

// monitor starts up a goroutine that monitors the memory of `m`. If
// panicHandler is set, any panic that escapes the user-supplied event handler
// (via m.watch) is recovered.
func (m *memoryWatchStage) monitor(ctx context.Context, panicHandler StagePanicHandler) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		if panicHandler != nil {
			defer func() {
				if p := recover(); p != nil {
					m.watchErr = panicHandler(p)
				}
			}()
		}

		m.watch(ctx)
	}()
}

func (m *memoryWatchStage) stopWatching() {
	m.cancel()
	m.wg.Wait()
}

// watch is a `memoryWatchFunc` that watches the memory usage of the
// specified `stage`.
func (m *memoryWatchStage) watch(ctx context.Context) {
	t := time.NewTicker(memoryPollInterval)
	defer t.Stop()

watchLoop:
	for {
		select {
		case <-ctx.Done():
			break watchLoop
		case <-t.C:
			if m.update(ctx) {
				// The stage was killed.
				break watchLoop
			}
		}
	}

	if m.observe {
		<-ctx.Done()
		m.reportPeakUsage()
	}
}

// update samples the current memory usage and updates internal stats.
// Return true if the stage was killed for exceeding the memory limit.
func (m *memoryWatchStage) update(ctx context.Context) bool {
	rss, err := m.stage.GetRSSAnon(ctx)
	if err != nil {
		m.handleGetRSSError(err)
		return false
	}

	m.consecutiveErrors = 0
	m.samples++
	if rss > m.maxRSS {
		m.maxRSS = rss
	}

	if m.limit != nil && rss >= *m.limit {
		m.killStage(rss)
		return true
	}

	return false
}

// handleGetRSSError deals with error `err` that happened when trying
// to get `stage`'s memory usage.
func (m *memoryWatchStage) handleGetRSSError(err error) {
	if !errors.Is(err, errProcessInfoMissing) {
		m.errCount++
		m.consecutiveErrors++
		if m.consecutiveErrors == 2 {
			m.eventHandler(&Event{
				Command: m.stage.Name(),
				Msg:     "error getting RSS",
				Err:     err,
			})
		}
	} else {
		m.consecutiveErrors = 0
	}
}

// killStage kills the stage and reports and event saying what it did.
func (m *memoryWatchStage) killStage(rss uint64) {
	// Guarantee the over-limit stage is killed even if
	// the user's event handler panics.
	defer m.stage.Kill(ErrMemoryLimitExceeded)

	m.eventHandler(&Event{
		Command: m.stage.Name(),
		Msg:     "stage exceeded allowed memory use",
		Err:     fmt.Errorf("stage exceeded allowed memory use"),
		Context: map[string]any{
			"limit": *m.limit,
			"used":  rss,
		},
	})
}

// reportPeakUsage sends an event reporting the peak usage that has
// been seen for `stage`.
func (m *memoryWatchStage) reportPeakUsage() {
	m.eventHandler(&Event{
		Command: m.stage.Name(),
		Msg:     "peak memory usage",
		Context: map[string]any{
			"max_rss_bytes": m.maxRSS,
			"samples":       m.samples,
			"errors":        m.errCount,
		},
	})
}
