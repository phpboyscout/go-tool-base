package templates

const SkeletonGoMod = `module {{ .ModulePath }}

go {{ .GoVersion }}


tool (
	github.com/phpboyscout/gtb
	github.com/golangci/golangci-lint/cmd/golangci-lint
	github.com/vektra/mockery/v3
)
`
