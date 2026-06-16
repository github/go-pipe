package pipe

import "fmt"

// EventError represents an error that could happen during the
// pipeline execution that we might want to report as an event.
type EventError struct {
	Command string
	Msg     string
	Err     error
	Context map[string]interface{}
}

type EventHandler func(err *EventError)

func (err *EventError) Error() string {
	if err.Msg == "" {
		return fmt.Sprintf("%s: %v", err.Command, err.Err)
	}
	return fmt.Sprintf("%s in stage %q: %v", err.Msg, err.Command, err.Err)
}

func (err *EventError) Unwrap() error {
	return err.Err
}

// Event is an alias for `EventError`, for reasons of backwards
// compatibility.
type Event = EventError
