package templates

const SkeletonGoMod = `module {{ .ModulePath }}

go {{ .GoVersion }}


tool (
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
)
{{- if .FrameworkReplace }}

// Development only: GTB_FRAMEWORK_REPLACE pointed this scaffold at a framework
// working tree. Regenerate without the variable set before publishing.
replace gitlab.com/phpboyscout/go-tool-base => {{ .FrameworkReplace }}
{{- end }}
`

// The tool block names only the framework's own commands. gtb is installed
// (spec 0197 D12); golangci-lint and mockery are run as the binaries the
// justfile and CI install, never through `go tool`, and a golangci-lint tool
// line pinned v1 (#31) and dragged an old viper whose cloud.google.com/go/compute
// made the module's tidy ambiguous once chat-gemini was linked.
