package errorhandling_test

import (
	"fmt"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
)

func ExampleWithUserHint() {
	err := errors.New("connection refused")
	hinted := errorhandling.WithUserHint(err, "Check that the server is running and the port is correct")

	fmt.Println(errors.FlattenHints(hinted))
	// Output: Check that the server is running and the port is correct
}

func ExampleWrapWithHint() {
	err := errors.New("file not found")
	wrapped := errorhandling.WrapWithHint(err, "loading config", "Run 'mytool init' to create the config file")

	fmt.Println(wrapped.Error())
	fmt.Println(errors.FlattenHints(wrapped))
	// Output:
	// loading config: file not found
	// Run 'mytool init' to create the config file
}

func ExampleNew() {
	handler := errorhandling.New(nil, nil)
	_ = handler // Use handler.Check, handler.Error, handler.Fatal, handler.Warn
}

func ExampleSlackHelp() {
	help := errorhandling.SlackHelp{
		Team:    "mycompany",
		Channel: "#dev-support",
	}

	fmt.Println(help.SupportMessage())
	// Output: If the problem persists, reach out to #dev-support on the mycompany Slack workspace
}
