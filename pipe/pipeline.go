package pipe

// Pipeline represents a Unix-like pipe that can include multiple
// stages, including external processes but also and stages written in
// Go.
type Pipeline struct {
	*runner

	p *Pipe
}

type NewPipeFn func(opts ...Option) *Pipeline

// NewPipeline returns a Pipeline struct with all of the `options`
// applied.
func New(options ...Option) *Pipeline {
	p := NewPipe("")

	return &Pipeline{
		runner: newRunner(p, options...),
		p:      p,
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
