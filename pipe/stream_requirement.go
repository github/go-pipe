package pipe

import "fmt"

// StreamRequirement describes a `Stage`'s requirement for its stdin
// or stdout, namely whether it can be anything, whether it should
// preferably be an `*os.File`, or whether it must be `nil`. The zero
// value `StreamAcceptAny` is a valid value that indicates that the
// stage has no particular requirements or preferences for its
// stdin/stdout, such as a typical `Function` stage.
type StreamRequirement int

const (
	// StreamAcceptAny indicates that the stage hasn't declared what
	// kind of stream it requires, maybe even `nil`.
	StreamAcceptAny StreamRequirement = iota

	// StreamPreferFile indicates that the stage prefers the
	// corresponding stream to be backed by an `*os.File` (a real file
	// descriptor), but it can work with any io.Reader/io.Writer.
	StreamPreferFile

	// StreamForbidden indicates that the stage requires the
	// corresponding stream to be nil. It won't read/write the stream
	// or close it.
	StreamForbidden
)

// Validate checks that `req` has a valid value and returns an error
// otherwise.
func (requirement StreamRequirement) Validate() error {
	switch requirement {
	case StreamAcceptAny, StreamPreferFile, StreamForbidden:
		return nil
	default:
		return fmt.Errorf("invalid stream requirement %d", requirement)
	}
}
