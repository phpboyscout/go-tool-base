package root

import (
	"io"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The root pre-run's two prompts (run the update now? opt into telemetry?)
// are gated on a terminal and answered at accessible prompts here, which is
// parallel-safe (spec 0198 D3).

// promptIO is a terminal in accessible mode answering one confirm.
func promptIO(answer string) p.IO {
	return formtest.AccessibleTTY(formtest.Answers(answer))
}

// nonInteractiveIO is a stdin nobody is typing at: both prompts are skipped
// without touching it.
func nonInteractiveIO() p.IO {
	return p.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
}
