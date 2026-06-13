package pipe

import "fmt"

type StreamRequirement int

const (
	// StreamOptional means the stream may be connected or nil.
	StreamOptional StreamRequirement = iota

	// StreamForbidden means the stream must be nil.
	StreamForbidden
)

// validate checks that `req` has a valid value and returns an error
// otherwise.
func (req StreamRequirement) validate() error {
	switch req {
	case StreamOptional, StreamForbidden:
		return nil
	default:
		return fmt.Errorf("invalid stream requirement %d", req)
	}
}
