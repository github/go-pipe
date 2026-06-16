package pipe

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Pipe is a `Stage` that consists of a bunch of other `Stage`s that
// are piped together and run in parallel.
type Pipe struct {
	name string

	stages []Stage

	cancel func()

	oneUse oneUse
}

var (
	_ Stage = (*Pipe)(nil)
)

// NewPipe returns an initialized `*Pipe` with the specified `name`.
func NewPipe(name string) *Pipe {
	return &Pipe{
		name:   name,
		oneUse: oneUse{thing: "Pipe"},
	}
}

func (p *Pipe) Name() string {
	return p.name
}

func (p *Pipe) Requirements() StageRequirements {
	if len(p.stages) == 0 {
		return StageRequirements{}
	}

	return StageRequirements{
		Stdin:  p.stages[0].Requirements().Stdin,
		Stdout: p.stages[len(p.stages)-1].Requirements().Stdout,
	}
}

// Add appends one or more stages to the pipe.
func (p *Pipe) Add(stages ...Stage) {
	p.oneUse.assertNotStarted("modify a pipe")

	p.stages = append(p.stages, stages...)
}

// AddWithIgnoredError appends one or more stages, suppressing any
// errors from those stages that match `em`.
func (p *Pipe) AddWithIgnoredError(em ErrorMatcher, stages ...Stage) {
	p.oneUse.assertNotStarted("modify a pipe")

	for _, stage := range stages {
		p.stages = append(p.stages, IgnoreError(stage, em))
	}
}

// Start starts the commands in the pipe. If `Start()` exits without
// an error, `Wait()` must also be called, to allow all resources to
// be freed.
//
// If `Start()` returns an error, `Wait()` must not be called. Before
// returning an error, `Start()` cancels and waits for any stages that
// were started, closes any inter-stage pipes that the pipe owns, and
// closes stdout if it was supplied with `WithStdoutCloser()`. Streams
// supplied with `WithStdin()` or `WithStdout()` remain owned by the
// caller and are not closed by the pipe.
func (p *Pipe) Start(
	ctx context.Context, opts StageOptions,
	stdin *InputStream, stdout *OutputStream,
) (theErr error) {
	p.oneUse.assertStarting("start")

	// We might need to cancel sub-stages if not all of them start up
	// correctly:
	ctx, p.cancel = context.WithCancel(ctx)

	// Be sure to free resources if startup isn't successful:
	defer func() {
		if theErr != nil {
			p.cancel()
		}
	}()

	if len(p.stages) == 0 {
		// This is pretty pointless, but handle it by copying stdin to
		// stdout.

		if stdin == nil || stdout == nil {
			// If `stdin` and `stout` were not both provided, then
			// there's nothing to do except close the other one if it
			// was provided. Note that if both closes are successful,
			// then as far as the caller is concerned this counts as a
			// successful start, and will therefore call `Wait()`,
			// which also does the right thing.
			return errors.Join(
				stdin.Close(),
				stdout.Close(),
			)
		}

		// Add a stage to do the copying, then proceed as usual:
		p.stages = append(p.stages, Function(
			"identity",
			func(_ context.Context, _ Env, stdin io.Reader, stdout io.Writer) error {
				if stdin == nil {
					return nil
				}
				_, err := io.Copy(stdout, stdin)
				return err
			},
		))
	}

	// We need to decide how to start the stages, especially what
	// pipes to use to connect adjacent stages (`os.Pipe()` vs.
	// `io.Pipe()`) based on the two stages' requirements.
	stageJoiners := make([]stageJoiner, len(p.stages)+1)

	// Arrange for the input of the 0th stage to come from `stdin`:
	stageJoiners[0].nextStdin = stdin

	// Arrange for the output of the last stage to go to `stdout`:
	stageJoiners[len(p.stages)].prevStdout = stdout

	// closePipes closes all of the streams that are currently stored
	// in the joiners. This should be called if startup fails. As we
	// call `Stage.Start()` and pass that method streams, we clear
	// them from the corresponding joiners to avoid closing them
	// twice.
	closePipes := func() {
		for _, sj := range stageJoiners {
			_ = sj.closePipe()
		}
	}

	// Store the stages in the joiners, and verify that the stages'
	// requirements are well-formed:
	for i, s := range p.stages {
		// Make sure that the stage's requirements are well-formed:
		requirements := s.Requirements()
		if err := requirements.Stdin.Validate(); err != nil {
			closePipes()
			return fmt.Errorf(
				"stage %q has invalid stdin requirement: %w", s.Name(), err,
			)
		}
		if err := requirements.Stdout.Validate(); err != nil {
			closePipes()
			return fmt.Errorf(
				"stage %q has invalid stdout requirement: %w", s.Name(), err,
			)
		}

		stageJoiners[i].nextStage = s
		stageJoiners[i].nextStageReq = requirements
		stageJoiners[i+1].prevStage = s
		stageJoiners[i+1].prevStageReq = requirements
	}

	// Check that each of the stages' requirements are satisfiable:
	for i := range stageJoiners {
		if err := stageJoiners[i].validate(); err != nil {
			closePipes()
			return err
		}
	}

	// Create the "inner" pipes (i.e, all but the first and last
	// `stageJoiners`):
	for i := 1; i < len(stageJoiners)-1; i++ {
		if err := stageJoiners[i].createPipe(); err != nil {
			closePipes()
			return err
		}
	}

	// We're about to start up the stages, one by one. If something
	// goes wrong during that process, `abort` should be called to
	// kill any stages that have already been started and to close any
	// pipes that have not yet been passed to a stage. `i` is the
	// index of the stage that failed to start. If the stage already
	// received its streams, it is responsible for closing them.
	abort := func(i int, err error) error {
		closePipes()

		// Kill and wait for any stages that have been started
		// already to finish:
		p.cancel()
		for _, s := range p.stages[:i] {
			_ = s.Wait()
		}
		return &EventError{
			Command: p.stages[i].Name(),
			Msg:     "failed to start pipe stage",
			Err:     err,
		}
	}

	// Loop over all of the stages, starting them in order.
	for i, s := range p.stages {
		prevSJ := &stageJoiners[i]
		nextSJ := &stageJoiners[i+1]

		err := s.Start(ctx, opts, prevSJ.nextStdin, nextSJ.prevStdout)

		// Even if that stage failed to start, we are no longer
		// responsible for closing its streams:
		prevSJ.nextStdin = nil
		nextSJ.prevStdout = nil

		if err != nil {
			return abort(i, err)
		}
	}

	return nil
}

// Wait waits for each stage in the pipe to exit.
func (p *Pipe) Wait() error {
	p.oneUse.assertStarted("wait")

	if len(p.stages) == 0 {
		// There was nothing to do, and we did it brilliantly!
		return nil
	}

	// Make sure that all of the cleanup eventually happens:
	defer p.cancel()

	var earliestStageErr error
	var earliestFailedStage Stage

	finishedEarly := false
	for i := len(p.stages) - 1; i >= 0; i-- {
		s := p.stages[i]
		err := s.Wait()

		// Handle errors:
		switch {
		case err == nil:
			// No error to handle. But unset the `finishedEarly` flag,
			// because earlier stages shouldn't be affected by the
			// later stage that finished early.
			finishedEarly = false
			continue

		case errors.Is(err, FinishEarly):
			// Note `FinishEarly` errors, because that is how a stage
			// informs us that it intentionally finished early.
			// Moreover, if we see a `FinishEarly` error, ignore any
			// pipe error from the immediately preceding stage,
			// because it probably came from trying to write to this
			// stage after this stage closed its stdin.
			finishedEarly = true
			continue

		case IsPipeError(err):
			switch {
			case finishedEarly:
				// A successor stage finished early. It is common for
				// this to cause earlier stages to fail with pipe
				// errors. Such errors are uninteresting, so ignore
				// them. Leave the `finishedEarly` flag set, because
				// the preceding stage might get a pipe error from
				// trying to write to this one.
			case earliestStageErr != nil:
				// A later stage has already reported an error. This
				// means that we don't want to report the error from
				// this stage:
				//
				// * If the later error was also a pipe error: we want
				//   to report the _last_ pipe error seen, which would
				//   be the one already recorded.
				//
				// * If the later error was not a pipe error: non-pipe
				//   errors are always considered more important than
				//   pipe errors, so again we would want to keep the
				//   error that is already recorded.
			default:
				// In this case, the pipe error from this stage is the
				// most important error that we have seen so far, so
				// remember it:
				earliestFailedStage, earliestStageErr = s, err
			}

		default:
			// This stage exited with a non-pipe error. If multiple
			// stages exited with such errors, we want to report the
			// one that is most informative. We take that to be the
			// error from the earliest failing stage. Since we are
			// iterating through stages in reverse order, overwrite
			// any existing remembered errors (which would have come
			// from a later stage):
			earliestFailedStage, earliestStageErr = s, err
			finishedEarly = false
		}
	}

	if earliestStageErr != nil {
		return &EventError{
			Command: earliestFailedStage.Name(),
			Msg:     "command failed",
			Err:     earliestStageErr,
		}
	}

	if finishedEarly {
		// The earliest stage finished early, so report that as our
		// final error. This might cause pipe errors from earlier
		// stages to get suppressed. If this is the top-level stage,
		// then this `FinishEarly` error will get ignored by
		// `Pipeline.Wait()`.
		return FinishEarly
	}

	return nil
}
