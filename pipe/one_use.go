package pipe

import (
	"fmt"
	"sync/atomic"
)

// oneUse is a helper class that can be used to enforce consistency
// checks that some `thing` is only started once, and can also check
// that it is in the correct state (either not started yet, or already
// started). Any violations of the assertions cause panics; this is
// only meant as a sanity consistency check.
type oneUse struct {
	// thing is a description of the thing that should only be started
	// once.
	thing string

	// started records whether the thing has been started yet.
	started atomic.Bool
}

// assertNotStarted panics if the thing has already been started.
// `action` is used in the `panic` message to describe the action that
// is being attempted.
func (ou *oneUse) assertNotStarted(action string) {
	if ou.started.Load() {
		panic(fmt.Sprintf("tried to %s after %s was already started", action, ou.thing))
	}
}

// assertStarting panics if the thing has already been started, and
// otherwise records that it has now been started. `action` is used in
// the `panic` message to describe the action that is being attempted.
func (ou *oneUse) assertStarting(action string) {
	if !ou.started.CompareAndSwap(false, true) {
		panic(fmt.Sprintf("tried to %s after %s was already started", action, ou.thing))
	}
}

// assertStarted panics if the thing has not yet been started.
// `action` is used in the `panic` message to describe the action that
// is being attempted.
func (ou *oneUse) assertStarted(action string) {
	if !ou.started.Load() {
		panic(fmt.Sprintf("tried to %s but %s hasn't been started", action, ou.thing))
	}
}
