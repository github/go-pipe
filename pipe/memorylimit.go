package pipe

import (
	"context"
	"errors"
	"io"
	"time"
)

const memoryPollInterval = time.Second

// ErrMemoryLimitExceeded is the error that will be used to kill a process, if
// necessary, from MemoryLimit.
var ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

// LimitableStage is the superset of Stage that must be implemented by stages
// passed to MemoryLimit and MemoryObsever.
type LimitableStage interface {
	Stage

	GetRSS(context.Context) (uint64, error)
	Kill(error)
}

// MemoryLimit watches the memory usage of the stage and stops it if it
// exceeds the given limit.
func MemoryLimit(stage Stage, byteLimit uint64) Stage {
	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		return stage
	}

	return &memoryWatchStage{
		nameSuffix: " with memory limit",
		stage:      limitableStage,
		watch:      killAtLimit(byteLimit),
	}
}

func killAtLimit(byteLimit uint64) memoryWatchFunc {
	return func(ctx context.Context, stage LimitableStage) {
		var consecutiveErrors int

		t := time.NewTicker(memoryPollInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rss, err := stage.GetRSS(ctx)
				if err != nil {
					consecutiveErrors++
					continue
				}
				consecutiveErrors = 0
				if rss < byteLimit {
					continue
				}
				stage.Kill(ErrMemoryLimitExceeded)
				return
			}
		}
	}
}

// MemoryObserver watches memory use of the stage and logs the maximum
// value when the stage exits.
func MemoryObserver(stage Stage) Stage {
	limitableStage, ok := stage.(LimitableStage)
	if !ok {
		return stage
	}

	return &memoryWatchStage{
		stage: limitableStage,
		watch: logMaxRSS(),
	}
}

func logMaxRSS() memoryWatchFunc {
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
				return
			case <-t.C:
				rss, err := stage.GetRSS(ctx)
				if err != nil {
					errors++
					consecutiveErrors++
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
	go m.watch(ctx, m.stage)
	return io, nil
}

func (m *memoryWatchStage) Wait() error {
	return m.stage.Wait()
}

func (m *memoryWatchStage) GetRSS(ctx context.Context) (uint64, error) {
	return m.stage.GetRSS(ctx)
}

func (m *memoryWatchStage) Kill(err error) {
	m.stage.Kill(err)
}
