# {{ .Name }}

{{ .Name | escapeMarkdown }} is a tool built with [gtb](https://gitlab.com/phpboyscout/go-tool-base).

## Installation

\`\`\`bash
go install {{ .Repo | escapeMarkdownCodeBlock }}@latest
\`\`\`

## Usage

\`\`\`bash
{{ .Name | escapeMarkdownCodeBlock }} --help
\`\`\`
