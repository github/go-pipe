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

// ErrMemoryLimitExceeded is the error that will be used to kill a process, if
// necessary, from MemoryLimit.
var ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

// LimitableStage is the superset of Stage that must be implemented by stages
// passed to MemoryLimit and MemoryObserver.
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
				if err != nil {
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

var _ LimitableStage = (*memoryWatchStage)(nil)

func (m *memoryWatchStage) Name() string {
	return m.stage.Name() + m.nameSuffix
}

func (m *memoryWatchStage) Start(ctx context.Context, env Env, stdin io.ReadCloser) (io.ReadCloser, error) {
	io, err := m.stage.Start(ctx, env, stdin)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)

	go func() {
		m.watch(ctx, m.stage)
		m.wg.Done()
	}()

	return io, nil
}

func (m *memoryWatchStage) Wait() error {
	if err := m.stage.Wait(); err != nil {
		return err
	}
	m.stopWatching()
	return nil
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
