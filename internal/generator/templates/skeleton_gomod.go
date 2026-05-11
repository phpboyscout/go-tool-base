package templates

const SkeletonGoMod = `module {{ .ModulePath }}

go {{ .GoVersion }}


tool (
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
	gitlab.com/phpboyscout/go-tool-base/cmd/gtb
	github.com/golangci/golangci-lint/cmd/golangci-lint
	github.com/vektra/mockery/v3
)
`
