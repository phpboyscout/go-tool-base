package templates

import (
	"slices"

	"github.com/dave/jennifer/jen"
)

// flagKind groups the flag types that share a default-value and zero-value
// shape, so the emitters switch on one thing.
type flagKind int

const (
	kindString flagKind = iota
	kindBool
	kindInteger
	kindFloat
	kindDuration
	kindStringSlice
	kindIntSlice
)

// flagTypeSpec is everything the templates know about one manifest flag
// type. It is the single table behind the options struct field, the pflag
// registration, the PreRunE getter, the default and the zero value, and the
// set validate.go accepts; six independent switches used to hold this and
// one of them lacked the sized integers (#47).
type flagTypeSpec struct {
	kind flagKind
	// goType renders the options struct field; a qualified type sets pkg.
	pkg, goType string
	// pflag's method suffix: <suffix>Var registers, Get<suffix> reads back.
	pflag string
}

var flagTypes = map[string]flagTypeSpec{
	"string":      {kind: kindString, goType: "string", pflag: "String"},
	"bool":        {kind: kindBool, goType: "bool", pflag: "Bool"},
	"int":         {kind: kindInteger, goType: "int", pflag: "Int"},
	"int32":       {kind: kindInteger, goType: "int32", pflag: "Int32"},
	"int64":       {kind: kindInteger, goType: "int64", pflag: "Int64"},
	"uint":        {kind: kindInteger, goType: "uint", pflag: "Uint"},
	"uint32":      {kind: kindInteger, goType: "uint32", pflag: "Uint32"},
	"uint64":      {kind: kindInteger, goType: "uint64", pflag: "Uint64"},
	"float64":     {kind: kindFloat, goType: "float64", pflag: "Float64"},
	"duration":    {kind: kindDuration, pkg: "time", goType: "Duration", pflag: "Duration"},
	"stringSlice": {kind: kindStringSlice, goType: "[]string", pflag: "StringSlice"},
	"stringslice": {kind: kindStringSlice, goType: "[]string", pflag: "StringSlice"},
	"stringArray": {kind: kindStringSlice, goType: "[]string", pflag: "StringArray"},
	"stringarray": {kind: kindStringSlice, goType: "[]string", pflag: "StringArray"},
	"intSlice":    {kind: kindIntSlice, goType: "[]int", pflag: "IntSlice"},
	"intslice":    {kind: kindIntSlice, goType: "[]int", pflag: "IntSlice"},
}

// FlagTypes lists every manifest flag type the templates render, sorted.
// The empty type means "string" and is not listed.
func FlagTypes() []string {
	names := make([]string, 0, len(flagTypes))
	for name := range flagTypes {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// specFor resolves a manifest flag type; empty is string. An unknown type
// cannot reach the templates (validate.go refuses it from FlagTypes), so it
// renders as a string rather than panicking.
func specFor(flagType string) flagTypeSpec {
	if flagType == "" {
		return flagTypes["string"]
	}

	if spec, ok := flagTypes[flagType]; ok {
		return spec
	}

	return flagTypes["string"]
}

// fieldType renders the options struct field type for a flag.
func (s flagTypeSpec) fieldType() jen.Code {
	if s.pkg != "" {
		return jen.Qual(s.pkg, s.goType)
	}

	return jen.Id(s.goType)
}
