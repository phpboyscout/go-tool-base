package controls_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/controls"
)

func ExampleNewController() {
	ctx := context.Background()

	// Create a controller (WithoutSignals for non-daemon usage)
	controller := controls.NewController(ctx, controls.WithoutSignals())

	// Register an HTTP service
	controller.Register("http-api",
		controls.WithStart(func(ctx context.Context) error {
			fmt.Println("HTTP server starting")
			return nil
		}),
		controls.WithStop(func(ctx context.Context) {
			fmt.Println("HTTP server stopping")
		}),
		controls.WithStatus(func() error {
			return nil // healthy
		}),
	)

	// Start all services
	controller.Start()

	// Graceful shutdown
	time.Sleep(10 * time.Millisecond)
	controller.Stop()
	controller.Wait()
}

func ExampleWithRestartPolicy() {
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	controller.Register("worker",
		controls.WithStart(func(ctx context.Context) error {
			return nil
		}),
		controls.WithRestartPolicy(controls.RestartPolicy{
			MaxRestarts:    3,
			InitialBackoff: time.Second,
			MaxBackoff:     30 * time.Second,
		}),
	)

	_ = controller
}

func ExampleWithLiveness() {
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	controller.Register("api",
		controls.WithStart(func(ctx context.Context) error { return nil }),
		controls.WithLiveness(func() error {
			// Check if the service can respond
			resp, err := http.Get("http://localhost:8080/healthz")
			if err != nil {
				return err
			}

			resp.Body.Close()

			return nil
		}),
	)

	_ = controller
}
