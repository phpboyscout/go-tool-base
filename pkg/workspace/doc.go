// Package workspace provides project root detection by walking up from a
// starting directory to find marker files (.gtb/manifest.yaml, go.mod, .git).
//
// This is a utility package — it has no integration with Props or the
// command lifecycle. Tool authors use it to scope commands to the current
// project context.
//
// # Usage
//
//	ws, err := workspace.DetectFromCWD(afero.NewOsFs(), workspace.DefaultMarkers...)
//	if err != nil {
//	    return errors.Wrap(err, "not inside a project")
//	}
//	fmt.Println("Project root:", ws.Root)
package workspace
