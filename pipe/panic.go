package pipe

import "fmt"

// StagePanicHandlerAware is an interface that Stages can implement to receive
// a panic handler from the pipeline. This is particularly useful for stages
// that execute work in a separate goroutine and need to manage panics occurring
// within that goroutine.
type StagePanicHandlerAware interface {
	SetPanicHandler(StagePanicHandler)
}

// StagePanicHandler is a function that handles panics in a pipeline's stages.
type StagePanicHandler func(err error)

// FromPanicValue converts a panic value to an error. If the panic value is
// already an error, it returns it directly. Otherwise, it wraps the value in
// a generic error.
func FromPanicValue(p any) error {
	var err error
	if e, ok := p.(error); ok {
		err = e
	} else {
		err = fmt.Errorf("%v", p)
	}
	return err
}
