package templates

import "github.com/dave/jennifer/jen"

// ExternalArgTokens is the closed injection vocabulary for the declarative
// external-command channel. Each token names a well-known dependency the
// generator can derive from the *props.Props value (p) when rendering an
// external constructor call onto the generated root.
//
// The set is deliberately CLOSED (see
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0182-external-command-attachment): keeping it a
// fixed vocabulary — rather than a free-form Go expression — keeps the generated
// root type-safe and reviewable and prevents the manifest from becoming an
// arbitrary-code injection vector. Anything the vocabulary cannot express uses
// the adapter channel instead.
//
// It is the SINGLE source of truth for the valid token set: the manifest
// validator (generator.validateExternalAttach) checks membership against it, and
// ExternalArgExpr renders each token, so the two sides cannot drift.
var ExternalArgTokens = []string{"logger", "props", "config", "fs", "version"}

// IsExternalArgToken reports whether tok is a member of the closed vocabulary.
func IsExternalArgToken(tok string) bool {
	for _, t := range ExternalArgTokens {
		if t == tok {
			return true
		}
	}

	return false
}

// SkeletonExternalAdapter returns the source of the external-command adapter
// escape hatch (pkg/cmd/external/attach.go). Unlike most generated files this is
// author-owned: gtb scaffolds it once and never overwrites it (it is preserved
// across regenerate), so the author can attach external command trees of any
// shape the declarative vocabulary cannot express. The generated root spreads
// external.Commands(p) into NewCmdRoot, so returned commands still pick up the
// framework middleware pipeline.
func SkeletonExternalAdapter() string {
	return `// Scaffolded once by gtb — this file is yours to edit.
//
// This is the external-command adapter escape hatch. gtb creates it once and
// never overwrites it (it is preserved across ` + "`gtb regenerate`" + `). Return the
// external Cobra command trees you want attached to the root here: build and,
// for *cobra.Command builders, wrap them with setup.Wrap; gate them on your own
// config if you like. The generated root spreads external.Commands(p) into
// NewCmdRoot, so the returned commands still receive the framework middleware.
package external

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Commands returns the external command trees to attach to the generated root.
// The default is empty; add your attachments here, for example:
//
//	return []*setup.Command{
//		setup.Wrap("", someexternal.NewCmdThing(p.GetLogger())),
//	}
func Commands(p *props.Props) []*setup.Command {
	_ = p

	return nil
}
`
}

// ExternalArgExpr renders one injection token to the Go expression, derived from
// the props value p, that is passed to an external constructor. It is the render
// counterpart of [ExternalArgTokens]; an unknown token (which the manifest
// validator rejects before render) falls back to p so the render can never
// panic. The token set here and in [ExternalArgTokens] must stay in lockstep —
// guarded by a test.
func ExternalArgExpr(token string) jen.Code {
	switch token {
	case "logger":
		return jen.Id("p").Dot("GetLogger").Call()
	case "config":
		return jen.Id("p").Dot("Config")
	case "fs":
		return jen.Id("p").Dot("FS")
	case "version":
		return jen.Id("p").Dot("Version")
	case "props":
		return jen.Id("p")
	default:
		return jen.Id("p")
	}
}
