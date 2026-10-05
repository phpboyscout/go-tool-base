package docs

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// failingFS serves a MapFS but refuses to open the named paths, so a walk sees
// an entry it then cannot read. It deliberately exposes only Open, so the
// fs helpers cannot bypass it through MapFS's ReadFile or ReadDir.
type failingFS struct {
	files fstest.MapFS
	fail  map[string]bool
}

func (f failingFS) Open(name string) (fs.File, error) {
	if f.fail[name] {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}

	return f.files.Open(name)
}

func newFailingFS(fail ...string) failingFS {
	set := make(map[string]bool, len(fail))
	for _, name := range fail {
		set[name] = true
	}

	return failingFS{
		files: fstest.MapFS{
			"good.md":     {Data: []byte("# Good\n\nneedle here\n")},
			"bad.md":      {Data: []byte("# Bad\n\nneedle here\n")},
			"sub/deep.md": {Data: []byte("# Deep\n\nneedle here\n")},
		},
		fail: set,
	}
}

func TestGetAllMarkdownContent_UnreadableFile(t *testing.T) {
	t.Parallel()

	_, err := GetAllMarkdownContent(newFailingFS("bad.md"))
	require.ErrorIs(t, err, fs.ErrPermission)
}

func TestGetAllMarkdownContent_UnreadableDirectory(t *testing.T) {
	t.Parallel()

	_, err := GetAllMarkdownContent(newFailingFS("sub"))
	require.ErrorIs(t, err, fs.ErrPermission)
}

func TestAskAI_ContentLoadFailure(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop()}

	_, err := AskAI(context.Background(), p, newFailingFS("bad.md"), "q", nil, nil, "unused")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load content")
}

func TestPerformSearch_SkipsUnreadableEntries(t *testing.T) {
	t.Parallel()

	t.Run("unreadable file is skipped", func(t *testing.T) {
		t.Parallel()

		m := &Model{fs: newFailingFS("bad.md")}

		msg, ok := m.performSearch("needle")().(SearchResultMessage)
		require.True(t, ok)

		paths := make([]string, 0, len(msg.Results))
		for _, r := range msg.Results {
			paths = append(paths, r.Path)
		}

		assert.ElementsMatch(t, []string{"good.md", "sub/deep.md"}, paths)
	})

	t.Run("unreadable directory stops the walk", func(t *testing.T) {
		t.Parallel()

		m := &Model{fs: newFailingFS("sub")}

		msg, ok := m.performSearch("needle")().(SearchResultMessage)
		require.True(t, ok)

		for _, r := range msg.Results {
			assert.NotEqual(t, "sub/deep.md", r.Path)
		}
	})

	t.Run("empty query returns no results", func(t *testing.T) {
		t.Parallel()

		m := &Model{fs: newFailingFS()}

		msg, ok := m.performSearch("")().(SearchResultMessage)
		require.True(t, ok)
		assert.Empty(t, msg.Results)
	})
}

type stubChatClient struct{ answer string }

func (s *stubChatClient) Add(context.Context, string, ...gochat.Media) error      { return nil }
func (s *stubChatClient) Ask(context.Context, string, any, ...gochat.Media) error { return nil }
func (s *stubChatClient) SetTools([]gochat.Tool) error                            { return nil }
func (s *stubChatClient) Usage() gochat.Usage                                     { return gochat.Usage{} }
func (s *stubChatClient) History() gochat.History                                 { return gochat.History{} }
func (s *stubChatClient) Chat(context.Context, string, ...gochat.Media) (string, error) {
	return s.answer, nil
}

type streamingStubChatClient struct{ stubChatClient }

func (s *streamingStubChatClient) StreamChat(_ context.Context, _ string, cb gochat.StreamCallback, _ ...gochat.Media) (string, error) {
	if err := cb(gochat.StreamEvent{Type: gochat.EventTextDelta, Delta: s.answer}); err != nil {
		return "", err
	}

	if err := cb(gochat.StreamEvent{Type: gochat.EventComplete}); err != nil {
		return "", err
	}

	return s.answer, nil
}

// registerStubProvider registers a uniquely named provider in the chat
// registry, which is process-global; callers use a name no other test uses.
func registerStubProvider(name string, client gochat.ChatClient) string {
	gochat.RegisterProvider(gochat.Provider(name), func(context.Context, gochat.Settings) (gochat.ChatClient, error) {
		return client, nil
	})

	return name
}

func TestAskAI_StreamingDeliversDeltas(t *testing.T) {
	t.Parallel()

	provider := registerStubProvider("pkg-docs-test-streaming", &streamingStubChatClient{stubChatClient{answer: "streamed"}})
	p := &props.Props{Logger: logger.NewNoop()}
	fsys := fstest.MapFS{"guide.md": {Data: []byte("# Guide\n")}}

	var deltas []string

	got, err := AskAI(context.Background(), p, fsys, "q", nil, func(d string) { deltas = append(deltas, d) }, provider)
	require.NoError(t, err)
	assert.Equal(t, "streamed", got)
	assert.Equal(t, []string{"streamed"}, deltas)

	got, err = AskAI(context.Background(), p, fsys, "q", nil, nil, provider)
	require.NoError(t, err, "a nil deltaFn is tolerated on the streaming path")
	assert.Equal(t, "streamed", got)
}

func TestAskAI_NonStreamingChat(t *testing.T) {
	t.Parallel()

	provider := registerStubProvider("pkg-docs-test-plain", &stubChatClient{answer: "plain"})
	p := &props.Props{Logger: logger.NewNoop()}
	fsys := fstest.MapFS{"guide.md": {Data: []byte("# Guide\n")}}

	got, err := AskAI(context.Background(), p, fsys, "q", nil, nil, provider)
	require.NoError(t, err)
	assert.Equal(t, "plain", got)
}

func TestGenerateNavFromFS_UnreadableRoot(t *testing.T) {
	t.Parallel()

	_, err := generateNavFromFS(newFailingFS("."), ".")
	require.ErrorIs(t, err, fs.ErrPermission)
}

func TestParseNavList_StringEntryWithoutHeading(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"plain.md": {Data: []byte("no heading here")}}

	nodes := parseNavList(fsys, []any{"plain.md"})

	require.Len(t, nodes, 1)
	assert.Equal(t, "plain.md", nodes[0].Title, "the path stands in for a missing heading")
}
