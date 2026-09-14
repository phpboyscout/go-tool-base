// Package generator implements the code generation engine that powers project
// scaffolding, command generation, and regeneration from manifest definitions.
//
// The core [Generator] type orchestrates a shared pipeline: asset generation,
// command registration, child re-registration, manifest updates, and documentation
// output. It operates on a manifest-first architecture where .gtb/manifest.yaml
// is the single source of truth for project structure.
//
// Key entry points are [Generator.GenerateSkeleton] for new projects,
// [Generator.GenerateCommand] for adding commands, and [Generator.RegenerateProject]
// for rebuilding registration files from an existing manifest.
package generator
