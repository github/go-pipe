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
// process, if necessary, from MemoryLimit.
var ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

// LimitableStage is the superset of `Stage` that must be implemented
// by stages passed to MemoryLimit and MemoryObserver.
type LimitableStage interface {
	Stage

	GetRSSAnon(context.Context) (uint64, error)
	Kill(error)
}

// MemoryLimit watches the memory usage of the stage and stops it if it
// exceeds the given limit.
func MemoryLimit(stage Stage, byteLimit uint64, eventHandler func(e *Event)) Stage {

	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryLimit usage",
			Err:     fmt.Errorf("invalid pipe.MemoryLimit usage"),
		})
		return stage
	}

	return &memoryWatchStage{
		nameSuffix: " with memory limit",
		stage:      limitableStage,
		watch:      killAtLimit(byteLimit, eventHandler),
	}
}

func killAtLimit(byteLimit uint64, eventHandler func(e *Event)) memoryWatchFunc {
	return func(ctx context.Context, stage LimitableStage) {
		var consecutiveErrors int

		t := time.NewTicker(memoryPollInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rss, err := stage.GetRSSAnon(ctx)
				if err != nil && !errors.Is(err, errProcessInfoMissing) {
					consecutiveErrors++
					if consecutiveErrors >= 2 {
						eventHandler(&Event{
							Command: stage.Name(),
							Msg:     "error getting RSS",
							Err:     err,
						})
					}
					continue
				}
				consecutiveErrors = 0
				if rss < byteLimit {
					continue
				}
				eventHandler(&Event{
					Command: stage.Name(),
					Msg:     "stage exceeded allowed memory use",
					Err:     fmt.Errorf("stage exceeded allowed memory use"),
					Context: map[string]interface{}{
						"limit": byteLimit,
						"used":  rss,
					},
				})
				stage.Kill(ErrMemoryLimitExceeded)
				return
			}
		}
	}
}

// MemoryLimitWithObserver combines MemoryLimit and MemoryObserver in
// one goroutine. It watches the memory usage of the stage, stops it
// if it exceeds the given limit, and logs the peak memory usage when
// the stage exits.
func MemoryLimitWithObserver(stage Stage, byteLimit uint64, eventHandler func(e *Event)) Stage {
	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryLimitWithObserver usage",
			Err:     fmt.Errorf("invalid pipe.MemoryLimitWithObserver usage"),
		})
		return stage
	}

	return &memoryWatchStage{
		nameSuffix: " with memory limit",
		stage:      limitableStage,
		watch:      killAtLimitAndObserve(byteLimit, eventHandler),
	}
}

func killAtLimitAndObserve(byteLimit uint64, eventHandler func(e *Event)) memoryWatchFunc {
	return func(ctx context.Context, stage LimitableStage) {
		var (
			maxRSS                               uint64
			samples, errCount, consecutiveErrors int
			killed                               bool
		)

		t := time.NewTicker(memoryPollInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				eventHandler(&Event{
					Command: stage.Name(),
					Msg:     "peak memory usage",
					Context: map[string]interface{}{
						"max_rss_bytes": maxRSS,
						"samples":       samples,
						"errors":        errCount,
					},
				})
				return
			case <-t.C:
				if killed {
					continue
				}

				rss, err := stage.GetRSSAnon(ctx)
				if err != nil {
					if !errors.Is(err, errProcessInfoMissing) {
						errCount++
						consecutiveErrors++
						if consecutiveErrors == 2 {
							eventHandler(&Event{
								Command: stage.Name(),
								Msg:     "error getting RSS",
								Err:     err,
							})
						}
					} else {
						consecutiveErrors = 0
					}
					continue
				}

				consecutiveErrors = 0
				samples++
				if rss > maxRSS {
					maxRSS = rss
				}

				if rss >= byteLimit {
					eventHandler(&Event{
						Command: stage.Name(),
						Msg:     "stage exceeded allowed memory use",
						Err:     fmt.Errorf("stage exceeded allowed memory use"),
						Context: map[string]interface{}{
							"limit": byteLimit,
							"used":  rss,
						},
					})
					stage.Kill(ErrMemoryLimitExceeded)
					killed = true
				}
			}
		}
	}
}

// MemoryObserver watches memory use of the stage and logs the maximum
// value when the stage exits.
func MemoryObserver(stage Stage, eventHandler func(e *Event)) Stage {
	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		eventHandler(&Event{
			Command: stage.Name(),
			Msg:     "invalid pipe.MemoryObserver usage",
			Err:     fmt.Errorf("invalid pipe.MemoryObserver usage"),
		})
		return stage
	}

	return &memoryWatchStage{
		stage: limitableStage,
		watch: logMaxRSS(eventHandler),
	}
}

func logMaxRSS(eventHandler func(e *Event)) memoryWatchFunc {

	return func(ctx context.Context, stage LimitableStage) {
		var (
			maxRSS                             uint64
			samples, errors, consecutiveErrors int
		)

		t := time.NewTicker(memoryPollInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				eventHandler(&Event{
					Command: stage.Name(),
					Msg:     "peak memory usage",
					Context: map[string]interface{}{
						"max_rss_bytes": maxRSS,
						"samples":       samples,
						"errors":        errors,
					},
				})

				return
			case <-t.C:
				rss, err := stage.GetRSSAnon(ctx)
				if err != nil {
					errors++
					consecutiveErrors++
					if consecutiveErrors == 2 {
						eventHandler(&Event{
							Command: stage.Name(),
							Msg:     "error getting RSS",
							Err:     err,
						})
					}
					// don't log any more errors until we get rss successfully.
					continue
				}

				consecutiveErrors = 0
				samples++
				if rss > maxRSS {
					maxRSS = rss
				}
			}
		}
	}
}

type memoryWatchStage struct {
	nameSuffix string
	stage      LimitableStage
	watch      memoryWatchFunc
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

type memoryWatchFunc func(context.Context, LimitableStage)

var (
	_ LimitableStage         = (*memoryWatchStage)(nil)
	_ StagePanicHandlerAware = (*memoryWatchStage)(nil)
)

func (m *memoryWatchStage) Name() string {
	return m.stage.Name() + m.nameSuffix
}

func (m *memoryWatchStage) Preferences() StagePreferences {
	return m.stage.Preferences()
}

// SetPanicHandler forwards the handler to the wrapped stage if it
// implements `StagePanicHandlerAware`. Without this, wrapping a
// panicking stage in `MemoryLimit` / `MemoryObserver` /
// `MemoryLimitWithObserver` would silently bypass
// `WithStagePanicHandler` (the type assertion in `Pipeline.Start()`
// only sees this wrapper's methods, not the wrapped stage's
// `SetPanicHandler`), letting the panic propagate out of the
// goroutine and crash the host process.
func (m *memoryWatchStage) SetPanicHandler(ph StagePanicHandler) {
	if phs, ok := m.stage.(StagePanicHandlerAware); ok {
		phs.SetPanicHandler(ph)
	}
}

func (m *memoryWatchStage) Start(
	ctx context.Context, env Env, stdin io.ReadCloser, stdout io.WriteCloser,
) error {
	if err := m.stage.Start(ctx, env, stdin, stdout); err != nil {
		return err
	}

	m.monitor(ctx)

	return nil
}

// monitor starts up a goroutine that monitors the memory of `m`.
func (m *memoryWatchStage) monitor(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)

	go func() {
		m.watch(ctx, m.stage)
		m.wg.Done()
	}()
}

func (m *memoryWatchStage) Wait() error {
	defer m.stopWatching()
	return m.stage.Wait()
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
