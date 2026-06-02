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
type MemoryWatchOption func(*memoryWatchConfig)

type memoryWatchConfig struct {
	limit   *uint64 // non-nil enables kill-at-limit
	observe bool    // log peak RSS when the stage exits
}

// WithMemoryLimit makes MemoryWatch kill the stage when its RSS exceeds
// byteLimit.
func WithMemoryLimit(byteLimit uint64) MemoryWatchOption {
	return func(c *memoryWatchConfig) {
		c.limit = &byteLimit
	}
}

// WithPeakUsageLogging makes MemoryWatch log the peak RSS when the stage
// exits.
func WithPeakUsageLogging() MemoryWatchOption {
	return func(c *memoryWatchConfig) {
		c.observe = true
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
// StartOptions.PanicHandler and the stage keeps running unmonitored; see
// StartOptions.PanicHandler.
func MemoryWatch(stage Stage, eventHandler func(e *Event), opts ...MemoryWatchOption) Stage {
	var cfg memoryWatchConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryWatch usage",
			Err:     fmt.Errorf("invalid pipe.MemoryWatch usage"),
		})
		return stage
	}

	if cfg.limit == nil && !cfg.observe {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryWatch usage",
			Err: fmt.Errorf(
				"pipe.MemoryWatch requires WithMemoryLimit and/or WithPeakUsageLogging",
			),
		})
		return stage
	}

	nameSuffix := ""
	if cfg.limit != nil {
		nameSuffix = " with memory limit"
	}

	return &memoryWatchStage{
		nameSuffix: nameSuffix,
		stage:      limitableStage,
		watch:      cfg.watchFunc(eventHandler),
	}
}

func (c *memoryWatchConfig) watchFunc(eventHandler func(e *Event)) memoryWatchFunc {
	mw := memoryWatcher{
		cfg:          c,
		eventHandler: eventHandler,
	}

	return mw.watch
}

type memoryWatcher struct {
	cfg          *memoryWatchConfig
	eventHandler func(e *Event)

	maxRSS            uint64
	samples           int
	errCount          int
	consecutiveErrors int
}

// watch is a `memoryWatchFunc` that watches the memory usage of the
// specified `stage`.
func (mw *memoryWatcher) watch(ctx context.Context, stage LimitableStage) {
	t := time.NewTicker(memoryPollInterval)

watchLoop:
	for {
		select {
		case <-ctx.Done():
			break watchLoop
		case <-t.C:
			rss, err := stage.GetRSSAnon(ctx)
			if err != nil {
				mw.handleGetRSSError(stage, err)
				continue
			}

			mw.consecutiveErrors = 0
			mw.samples++
			if rss > mw.maxRSS {
				mw.maxRSS = rss
			}

			if mw.cfg.limit != nil && rss >= *mw.cfg.limit {
				mw.killStage(stage, rss)

				// After a kill we wait for `ctx.Done()` and then emit
				// the peak-usage event.
				break watchLoop
			}
		}
	}

	t.Stop()

	if mw.cfg.observe {
		<-ctx.Done()
		mw.reportPeakUsage(stage)
	}
}

// handleGetRSSError deals with error `err` that happened when trying
// to get `stage`'s memory usage.
func (mw *memoryWatcher) handleGetRSSError(stage LimitableStage, err error) {
	if !errors.Is(err, errProcessInfoMissing) {
		mw.errCount++
		mw.consecutiveErrors++
		if mw.consecutiveErrors == 2 {
			mw.eventHandler(&Event{
				Command: stage.Name(),
				Msg:     "error getting RSS",
				Err:     err,
			})
		}
	} else {
		mw.consecutiveErrors = 0
	}
}

// killStage kills `stage` and reports and event saying what it did.
func (mw *memoryWatcher) killStage(stage LimitableStage, rss uint64) {
	// Guarantee the over-limit stage is killed even if
	// the user's event handler panics.
	defer stage.Kill(ErrMemoryLimitExceeded)

	mw.eventHandler(&Event{
		Command: stage.Name(),
		Msg:     "stage exceeded allowed memory use",
		Err:     fmt.Errorf("stage exceeded allowed memory use"),
		Context: map[string]any{
			"limit": *mw.cfg.limit,
			"used":  rss,
		},
	})
}

// reportPeakUsage sends an event reporting the peak usage that has
// been seen for `stage`.
func (mw *memoryWatcher) reportPeakUsage(stage LimitableStage) {
	mw.eventHandler(&Event{
		Command: stage.Name(),
		Msg:     "peak memory usage",
		Context: map[string]any{
			"max_rss_bytes": mw.maxRSS,
			"samples":       mw.samples,
			"errors":        mw.errCount,
		},
	})
}

type memoryWatchStage struct {
	nameSuffix string
	stage      LimitableStage
	watch      memoryWatchFunc
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	watchErr   error
}

type memoryWatchFunc func(context.Context, LimitableStage)

var _ LimitableStage = (*memoryWatchStage)(nil)

func (m *memoryWatchStage) Name() string {
	return m.stage.Name() + m.nameSuffix
}

func (m *memoryWatchStage) Preferences() StagePreferences {
	return m.stage.Preferences()
}

func (m *memoryWatchStage) Start(
	ctx context.Context, env Env, stdin io.ReadCloser, stdout io.WriteCloser, opts StartOptions,
) error {
	if err := m.stage.Start(ctx, env, stdin, stdout, opts); err != nil {
		return err
	}

	m.monitor(ctx, opts.PanicHandler)

	return nil
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
		defer func() {
			if p := recover(); p != nil {
				if panicHandler == nil {
					panic(p)
				}
				m.watchErr = panicHandler(p)
			}
		}()
		m.watch(ctx, m.stage)
	}()
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

func (m *memoryWatchStage) stopWatching() {
	m.cancel()
	m.wg.Wait()
}
