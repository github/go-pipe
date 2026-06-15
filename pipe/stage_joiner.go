package pipe

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// stageJoiner is a helper type that helps join two adjacent stages
// together. stageJoiners[i] tells how to connect stage `i-1` to stage
// `i`. From the point of view of stages, `stageJoiners[i].nextStdin`
// and `stageJoiners[i+1].prevStdout` are the input and output
// streams, respectively, of `stage[i]`. The first and last elements
// of `stageJoiners` manage `p.stdin` and `p.stdout`, respectively.
// Schematically, the data flows through like this:
//
//	p.stdin == stageJoiners[0].nextStdin →
//	   stage[0] →
//	   stageJoiners[1].prevStdout → stageJoiners[1].nextStdin →
//	   stage[1] →
//	   stageJoiners[2].prevStdout → stageJoiners[2].nextStdin →
//	   stage[2] →
//	   ... →
//	   stageJoiners[i].prevStdout → stageJoiners[i].nextStdin →
//	   stage[i] →
//	   stageJoiners[i+1].prevStdout → stageJoiners[i+1].nextStdin →
//	   ... →
//	   stageJoiners[len(stages)-1].prevStdout → stageJoiners[len(stages)-1].nextStdin →
//	   stage[len(stages)-1] →
//	   stageJoiners[len(stages)].prevStdout == p.stdout
//
// In pseudo-Shell notation, the stages are run like this:
//
//	stage[0] <stageJoiners[0].nextStdin >stageJoiners[1].prevStdout
//	stage[1] <stageJoiners[1].nextStdin >stageJoiners[2].prevStdout
//	stage[2] <stageJoiners[2].nextStdin >stageJoiners[3].prevStdout
//	   ...
//	stage[i] <stageJoiners[i-1].nextStdin >stageJoiners[i].prevStdout
//	   ...
//	stage[len(stages)-1] <stageJoiners[len(stages)-1].nextStdin >p.stdout
type stageJoiner struct {
	// prevStage holds the stage that needs to write to the pipe.
	prevStage Stage

	// prevStageReq caches `prevStage.Requirements()` so that it
	// doesn't have to be recomputed. It is the zero value if
	// `prevStage` is nil.
	prevStageReq StageRequirements

	// prevStdout will be used as the stdout of `prevStage`. It is
	// usually the "write" end of the `(nextStdin, prevStdout)` pipe
	// pair, with the connected pipe ends in the same `stageJoiner`
	// instance.
	prevStdout *OutputStream

	// nextStage holds the stage that needs to read from the pipe.
	nextStage Stage

	// nextStageReq caches `nextStage.Requirements()` so that it
	// doesn't have to be recomputed. It is the zero value if
	// `nextStage` is nil.
	nextStageReq StageRequirements

	// nextStdin will be used as the stdin of `nextStage`. It is
	// usually the "read" end of the `(nextStdin, prevStdout)` pipe
	// pair.
	nextStdin *InputStream
}

// needFilePipe returns `true` if the pipe that joins the two adjacent
// stages should be an `os.Pipe()` rather than an `io.Pipe()`.
func (sj *stageJoiner) needFilePipe() bool {
	return sj.prevStageReq.Stdout == StreamPreferFile ||
		sj.nextStageReq.Stdin == StreamPreferFile
}

func (sj *stageJoiner) createPipe() error {
	var r io.ReadCloser
	var w io.WriteCloser
	if sj.needFilePipe() {
		var err error
		r, w, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("creating os.Pipe: %w", err)
		}
	} else {
		r, w = io.Pipe()
	}

	sj.prevStdout = ClosingOutput(w)
	sj.nextStdin = ClosingInput(r)

	return nil
}

// closePipe closes both ends of the pipe that was allocated by
// `createPipe()`. This should only be called if the corresponding
// stage's `Start()` method was never called (otherwise the stage is
// responsible for closing its stdin and stdout).
func (sj *stageJoiner) closePipe() error {
	return errors.Join(
		sj.prevStdout.Close(),
		sj.nextStdin.Close(),
	)
}

// validate verifies that the adjacent stages' stream requirements are
// satisfiable, in particular that a stage that forbids its stdin or
// stdout is not connected to anything.
func (sj *stageJoiner) validate() error {
	// `prevStage`'s stdout is connected if there is a `nextStage` to
	// consume it (in which case an inner pipe will be created) or if
	// a stream (`p.stdout`) has already been stored in `prevStdout`.
	if sj.prevStage != nil && sj.prevStageReq.Stdout == StreamForbidden &&
		(sj.nextStage != nil || sj.prevStdout != nil) {
		return fmt.Errorf(
			"stage %q forbids stdout, but stdout is connected", sj.prevStage.Name(),
		)
	}

	// `nextStage`'s stdin is connected if there is a `prevStage` to
	// produce it (in which case an inner pipe will be created) or if
	// a stream (`p.stdin`) has already been stored in `nextStdin`.
	if sj.nextStage != nil && sj.nextStageReq.Stdin == StreamForbidden &&
		(sj.prevStage != nil || sj.nextStdin != nil) {
		return fmt.Errorf(
			"stage %q forbids stdin, but stdin is connected", sj.nextStage.Name(),
		)
	}

	return nil
}
