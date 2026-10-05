package generate

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// defaultsResponder is a person accepting every default at accessible
// prompts: it answers the prompt just written, from a script for the
// questions that have no default, and otherwise with Enter, or 0 where a
// multi-select asks for it to confirm the selection.
type defaultsResponder struct {
	mu      sync.Mutex
	out     bytes.Buffer
	seen    int
	script  map[string]string
	pending []byte
	asked   []string
}

func (r *defaultsResponder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.out.Write(p)
}

func (r *defaultsResponder) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.pending) == 0 {
		prompt := r.out.String()[r.seen:]
		r.seen = r.out.Len()

		if len(r.asked) > 500 {
			return 0, io.EOF // a prompt that will not take its answer
		}

		r.asked = append(r.asked, prompt)
		r.pending = []byte(r.answer(prompt) + "\n")
	}

	n := copy(p, r.pending)
	r.pending = r.pending[n:]

	return n, nil
}

func (r *defaultsResponder) answer(prompt string) string {
	for title, answer := range r.script {
		if strings.Contains(prompt, title) {
			delete(r.script, title)

			return answer
		}
	}

	if strings.Contains(prompt, "Enter a number between 0 and") {
		return "0"
	}

	return ""
}

// huh's accessible runner asks hidden pages too (huh v2.0.3; huh#780). With
// no chat provider chosen the hidden "Default provider" select has no
// options, and asking it panicked.
func TestProjectWizard_AccessibleSkipsHiddenPages(t *testing.T) {
	t.Parallel()

	r := &defaultsResponder{script: map[string]string{"Project Name": "mytool", "Repository": "acme/mytool"}}
	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), IO: props.StdIO{Stdin: r, Stdout: r, Stderr: r, AccessibleMode: true}}
	o := &SkeletonOptions{Features: slices.Clone(generator.DefaultSelectedFeatures)}

	require.NotPanics(t, func() {
		require.NoError(t, o.runWizard(context.Background(), p))
	})

	transcript := strings.Join(r.asked, "")
	assert.Equal(t, "mytool", o.Name)
	assert.NotContains(t, transcript, "Default provider", "the AI pages are hidden without the ai feature")
	assert.NotContains(t, transcript, "Slack Channel", "the help channel pages are hidden without a channel")
	assert.NotContains(t, transcript, "Module path", "a hosted project derives its module path")

	// Accessible mode never evaluates the reactive bindings, so a page built
	// before the name was answered showed a prefix derived from nothing, and
	// the summary confirmed by "Generate now?" said none.
	assert.Contains(t, transcript, "MYTOOL (from the project name)")
	assert.Regexp(t, `Env prefix\s+MYTOOL`, transcript)
}

// The AI-prompt page is shown only when "Set AI Prompt" is confirmed; huh's
// accessible runner asked it regardless.
func TestCommandWizard_AccessibleSkipsTheDeclinedPromptPage(t *testing.T) {
	t.Parallel()

	r := &defaultsResponder{script: map[string]string{"Command Name": "deploy", "Short Description": "Deploy it"}}
	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), IO: props.StdIO{Stdin: r, Stdout: r, Stderr: r, AccessibleMode: true}}
	o := &CommandOptions{}

	require.NoError(t, o.runInteractivePrompt(context.Background(), p))

	assert.Equal(t, "deploy", o.Name)
	assert.False(t, o.AddPrompt)
	assert.Equal(t, 1, strings.Count(strings.Join(r.asked, ""), "AI Prompt"), "only the Set AI Prompt confirm, not the page")
}
