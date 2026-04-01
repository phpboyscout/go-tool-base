package workspace_test

import (
	"fmt"

	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/workspace"
)

func ExampleDetect() {
	fs := afero.NewMemMapFs()

	// Create a project with a go.mod
	_ = afero.WriteFile(fs, "/home/user/project/go.mod", []byte("module example"), 0o644)
	_ = fs.MkdirAll("/home/user/project/pkg/cmd", 0o755)

	// Detect from a nested directory
	ws, err := workspace.Detect(fs, "/home/user/project/pkg/cmd", workspace.DefaultMarkers)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Root:", ws.Root)
	fmt.Println("Marker:", ws.Marker)
	// Output:
	// Root: /home/user/project
	// Marker: go.mod
}

func ExampleDetect_customMarkers() {
	fs := afero.NewMemMapFs()

	_ = afero.WriteFile(fs, "/project/package.json", []byte("{}"), 0o644)
	_ = fs.MkdirAll("/project/src/components", 0o755)

	ws, err := workspace.Detect(fs, "/project/src/components", []string{"package.json"})
	if err != nil {
		return
	}

	fmt.Println("Root:", ws.Root)
	// Output:
	// Root: /project
}
