package pipe

// StagePanicHandlerAware is an interface that Stages can implement to receive
// a panic handler from the pipeline. This is particularly useful for stages
// that execute work in a separate goroutine and need to manage panics occurring
// within that goroutine.
type StagePanicHandlerAware interface {
	SetPanicHandler(StagePanicHandler)
}

// StagePanicHandler is a function that handles panics in a pipeline's stages.
type StagePanicHandler func(p any) error
