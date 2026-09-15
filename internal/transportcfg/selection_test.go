package transportcfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type gtbOpt func(*Selection)

type transportOpt string

func TestSplitOptions(t *testing.T) {
	t.Parallel()

	withPrefix := gtbOpt(func(s *Selection) { s.Prefix = "server.admin" })
	port := 8081
	withPort := gtbOpt(func(s *Selection) { s.Port = &port })

	sel, tOpts, unknown := SplitOptions[gtbOpt, transportOpt]([]any{
		withPrefix, transportOpt("a"), withPort, transportOpt("b"), 42, "stray",
	})

	require.Equal(t, "server.admin", sel.Prefix)
	require.Equal(t, &port, sel.Port)
	require.Equal(t, []transportOpt{"a", "b"}, tOpts, "transport options keep their order")
	require.Equal(t, []any{42, "stray"}, unknown)
	require.Equal(t, "int, string", UnknownOptionTypes(unknown))
}

func TestSelection_ResolvedPrefix(t *testing.T) {
	t.Parallel()

	require.Equal(t, "server.http", Selection{}.ResolvedPrefix("server.http"))
	require.Equal(t, "server.admin", Selection{Prefix: "server.admin"}.ResolvedPrefix("server.http"))
}

func TestSelect(t *testing.T) {
	t.Parallel()

	withPrefix := gtbOpt(func(s *Selection) { s.Prefix = "server.admin" })

	sel, rest := Select[gtbOpt]([]any{"a", withPrefix, 2})
	require.Equal(t, "server.admin", sel.Prefix)
	require.Equal(t, []any{"a", 2}, rest)
}
