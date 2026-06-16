package pipe

import (
	"context"
)

// Pipeline represents a Unix-like pipe that can include multiple
// stages, including external processes but also and stages written in
// Go.
type Pipeline struct {
	r *runner

	p *Pipe

	wait WaitFunc
}

// NewPipeline returns a Pipeline struct with all of the `options`
// applied. Since `Pipeline` doesn't allow external access to its
// `Runner`, it permits any `StartOption`s as options (not only
// `RunnerOption`s).
func New(options ...Option) *Pipeline {
	p := NewPipe("pipeline")
	r := newRunner(p, options...)
	return &Pipeline{
		r: r,
		p: p,
	}
}

// Add appends one or more stages to the pipeline.
func (p *Pipeline) Add(stages ...Stage) {
	p.p.Add(stages...)
}

// AddWithIgnoredError appends one or more stages that are ignoring
// the passed in error to the pipeline.
func (p *Pipeline) AddWithIgnoredError(em ErrorMatcher, stages ...Stage) {
	p.p.AddWithIgnoredError(em, stages...)
}

func (p *Pipeline) Start(ctx context.Context, options ...Option) error {
	p.r.applyOptions(options...)
	wait, err := p.r.start(ctx)
	if err != nil {
		return err
	}
	p.wait = wait
	return nil
}

func (p *Pipeline) Wait() error {
	return p.wait()
}

func (p *Pipeline) Run(ctx context.Context, options ...Option) error {
	p.r.applyOptions(options...)
	return p.r.run(ctx)
}

func (p *Pipeline) Output(ctx context.Context, options ...Option) ([]byte, error) {
	p.r.applyOptions(options...)
	return p.r.output(ctx)
}
