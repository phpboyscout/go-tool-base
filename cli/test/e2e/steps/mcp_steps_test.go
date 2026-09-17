package steps_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// The MCP steps drive the built gtb over a real stdio session, the way an
// editor would: initialize, then one request. What comes back lands on the
// CLI world's stdout and exit code so the ordinary Then steps apply.

func initMCPSteps(ctx *godog.ScenarioContext) {
	ctx.Step(`^I list the MCP tools over a stdio session with gtb$`, iListTheMCPToolsOverStdio)
	ctx.Step(`^I call the MCP tool "([^"]*)" with arguments '([^']*)' over a stdio session with gtb$`, iCallTheMCPToolOverStdio)
}

type rpcSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *strings.Builder
	id     int
}

func openSession(ctx context.Context, w *cliWorld) (*rpcSession, error) {
	args := []string{"mcp", "start", "--log-level", "warn", "--ci", "--config", filepath.Join(w.configDir, "config.yaml")}

	cmd := exec.CommandContext(ctx, w.binaryPath, args...) //nolint:gosec // test-only
	cmd.Env = append(os.Environ(), "HOME="+w.configDir)

	stderr := &strings.Builder{}
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gtb mcp start: %w", err)
	}

	s := &rpcSession{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}

	if _, err := s.call("initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"gtb-e2e","version":"0"}}`); err != nil {
		return nil, err
	}

	_, err = fmt.Fprintln(s.stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	return s, err
}

func (s *rpcSession) call(method, params string) (json.RawMessage, error) {
	s.id++

	if _, err := fmt.Fprintf(s.stdin, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", s.id, method, params); err != nil {
		return nil, err
	}

	for {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read %s response: %w\nstderr: %s", method, err, s.stderr.String())
		}

		var message map[string]json.RawMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, fmt.Errorf("not JSON-RPC: %s", line)
		}

		if id, ok := message["id"]; ok && string(id) == fmt.Sprint(s.id) {
			if rpcErr, failed := message["error"]; failed {
				return nil, fmt.Errorf("%s failed: %s", method, rpcErr)
			}

			return message["result"], nil
		}
	}
}

func (s *rpcSession) close() error {
	_ = s.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()

		return fmt.Errorf("gtb mcp start did not exit after stdin closed\nstderr: %s", s.stderr.String())
	}
}

func iListTheMCPToolsOverStdio(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	session, err := openSession(ctx, w)
	if err != nil {
		return ctx, err
	}

	result, err := session.call("tools/list", `{}`)
	if err != nil {
		return ctx, err
	}

	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return ctx, err
	}

	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}

	w.stdout = strings.Join(names, ",")
	w.stderr = session.stderr.String()
	w.exitCode = 0

	if err := session.close(); err != nil {
		w.exitCode = 1

		return ctx, err
	}

	return ctx, nil
}

func iCallTheMCPToolOverStdio(ctx context.Context, name, arguments string) (context.Context, error) {
	w := getCLIWorld(ctx)

	session, err := openSession(ctx, w)
	if err != nil {
		return ctx, err
	}

	result, err := session.call("tools/call", fmt.Sprintf(`{"name":"call_tool","arguments":{"name":%q,"arguments":%s}}`, name, arguments))
	if err != nil {
		return ctx, err
	}

	var call struct {
		IsError    bool `json:"isError"`
		Structured struct {
			Value struct {
				Stdout   string `json:"stdout"`
				Stderr   string `json:"stderr"`
				ExitCode int    `json:"exitCode"`
			} `json:"value"`
			Diagnostics struct {
				Stdout   string `json:"stdout"`
				Stderr   string `json:"stderr"`
				ExitCode int    `json:"exitCode"`
			} `json:"diagnostics"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(result, &call); err != nil {
		return ctx, fmt.Errorf("decode call result: %w\n%s", err, result)
	}

	view := call.Structured.Value
	if call.IsError {
		view = call.Structured.Diagnostics
	}

	w.stdout = view.Stdout
	w.stderr = view.Stderr
	w.exitCode = view.ExitCode

	return ctx, session.close()
}
