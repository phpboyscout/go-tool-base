package output_test

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/output"
)

func ExampleSpin() {
	// Spin shows a spinner while the function executes.
	// In non-interactive environments (CI), it prints plain text instead.
	err := output.Spin(context.Background(), "Loading data", func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	if err != nil {
		fmt.Println("Error:", err)
	}
}

func ExampleSpinWithResult() {
	result, err := output.SpinWithResult(context.Background(), "Fetching version", func(ctx context.Context) (string, error) {
		return "v1.2.3", nil
	})

	if err == nil {
		fmt.Println("Version:", result)
	}
}

func ExampleNewProgress() {
	// Progress tracks a known-total operation.
	bar := &output.Progress{}
	_ = bar // In real usage: bar = output.NewProgress(100, "Processing files")

	// bar.Increment()    // advance by 1
	// bar.IncrementBy(5) // advance by 5
	// bar.Done()         // mark complete
}

func ExampleNewStatus() {
	var buf bytes.Buffer

	s := &output.Status{}
	_ = s
	_ = buf
	// In real usage:
	// s := output.NewStatus()
	// s.Update("Loading configuration")
	// s.Success("Configuration loaded")
	// s.Update("Connecting to API")
	// s.Warn("Connection slow")
	// s.Done()
}

func ExampleNewTableWriter() {
	type release struct {
		Name    string `table:"NAME,sortable"`
		Version string `table:"VERSION"`
		Status  string `table:"STATUS"`
	}

	rows := []release{
		{Name: "my-tool", Version: "v1.2.3", Status: "up to date"},
		{Name: "other", Version: "v0.9.1", Status: "update available"},
	}

	var buf bytes.Buffer
	tw := output.NewTableWriter(&buf, output.FormatText, output.WithNoTruncation())

	if err := tw.WriteRows(rows); err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Print(buf.String())
	// Output:
	// NAME      VERSION   STATUS
	// my-tool   v1.2.3    up to date
	// other     v0.9.1    update available
}
