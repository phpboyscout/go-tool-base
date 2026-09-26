package generator

import (
	"go/token"
	"strconv"
	"strings"
	"time"

	"github.com/dave/dst"
)

// applyLiteralToolField recovers the props.Tool fields that the generator renders
// directly into the cmd.go Tool literal (beyond Name/Description/Features/
// ReleaseSource): EnvPrefix, UpdatePolicy, UpdateCheckInterval, Help, Telemetry,
// and Bootstrap. It is the scanner half of skeleton_root.go's buildToolDict, so
// a from-scratch manifest rebuild reproduces these author-set properties.
//
// Fields the renderer does NOT put in the literal (Signing beyond its embedded
// keys, ModulePublished, template provenance) are recovered elsewhere — see the
// annotated provenance file (D9) and the source-inspection recoveries.
func applyLiteralToolField(mp *ManifestProperties, fieldName string, value dst.Expr) {
	switch fieldName {
	case "EnvPrefix":
		if v, ok := stringLitValue(value); ok {
			mp.EnvPrefix = v
		}
	case "UpdatePolicy":
		mp.UpdatePolicy = updatePolicyFromExpr(value)
	case "UpdateCheckInterval":
		if d, ok := durationFromExpr(value); ok {
			mp.UpdateCheckInterval = d.String()
		}
	case "MCP":
		mp.MCP.Mode = mcpModeFromLiteral(value)
	case "Config":
		extractConfigSpecLiteral(value, &mp.Config)
	default:
		applyBlockToolField(mp, fieldName, value)
	}
}

// applyBlockToolField reads the fields whose value is a nested literal.
func applyBlockToolField(mp *ManifestProperties, fieldName string, value dst.Expr) {
	switch fieldName {
	case "Help":
		extractHelpLiteral(value, &mp.Help)
	case "Telemetry":
		extractTelemetryLiteral(value, &mp.Telemetry)
	case "Bootstrap":
		extractBootstrapLiteral(value, &mp.Bootstrap)
	case "Signing":
		extractSigningLiteral(value, &mp.Signing)
	}
}

// mcpModeFromLiteral reads props.MCPConfig{Mode: props.MCPDirect}; anything
// else is the compact default and records nothing.
func mcpModeFromLiteral(value dst.Expr) string {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return ""
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		if key, ok := kv.Key.(*dst.Ident); ok && key.Name == "Mode" {
			if sel, ok := kv.Value.(*dst.SelectorExpr); ok && sel.Sel.Name == "MCPDirect" {
				return "direct"
			}
		}
	}

	return ""
}

// extractSigningLiteral recovers the author baselines a props.SigningConfig
// literal carries. EmbeddedKeys is a call into the trust-keys package, not a
// setting, and is skipped.
func extractSigningLiteral(value dst.Expr, sg *ManifestSigning) {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		if key, ok := kv.Key.(*dst.Ident); ok && key.Name == "RequireChecksum" {
			sg.RequireChecksum = boolPtrCallValue(kv.Value)
		}
	}
}

// boolPtrCallValue reads the argument of a props.BoolPtr(true|false) call,
// the form the renderer emits for a tri-state baseline.
func boolPtrCallValue(value dst.Expr) bool {
	call, ok := value.(*dst.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}

	return boolLitValue(call.Args[0])
}

// updatePolicyFromExpr maps a props.UpdatePolicy* selector back to its manifest
// value, inverting skeleton_root.go's updatePolicyConst.
func updatePolicyFromExpr(value dst.Expr) string {
	sel, ok := value.(*dst.SelectorExpr)
	if !ok {
		return ""
	}

	switch sel.Sel.Name {
	case "UpdatePolicyPrompt":
		return "prompt"
	case "UpdatePolicyEnabled":
		return "enabled"
	default:
		return ""
	}
}

// durationFromExpr evaluates the update-check-interval expression the renderer
// emits — `N * time.Unit` or `time.Duration(n)` — back to a time.Duration,
// inverting skeleton_root.go's updateCheckIntervalCode.
func durationFromExpr(value dst.Expr) (time.Duration, bool) {
	switch v := value.(type) {
	case *dst.BinaryExpr:
		// N * time.Unit
		n, ok := intLitValue(v.X)
		if !ok || v.Op.String() != "*" {
			return 0, false
		}

		unit, ok := durationUnit(v.Y)
		if !ok {
			return 0, false
		}

		return time.Duration(n) * unit, true
	case *dst.CallExpr:
		// time.Duration(n)
		if len(v.Args) != 1 {
			return 0, false
		}

		n, ok := intLitValue(v.Args[0])
		if !ok {
			return 0, false
		}

		return time.Duration(n), true
	default:
		return 0, false
	}
}

// durationUnit maps a time.Hour/Minute/Second selector to its time.Duration.
func durationUnit(expr dst.Expr) (time.Duration, bool) {
	sel, ok := expr.(*dst.SelectorExpr)
	if !ok {
		return 0, false
	}

	switch sel.Sel.Name {
	case "Hour":
		return time.Hour, true
	case "Minute":
		return time.Minute, true
	case "Second":
		return time.Second, true
	default:
		return 0, false
	}
}

// extractHelpLiteral recovers a ManifestHelp from a props.SlackHelp{...}
// or TeamsHelp{...} composite literal, inverting buildToolDict's Help switch.
func extractHelpLiteral(value dst.Expr, help *ManifestHelp) {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return
	}

	sel, ok := comp.Type.(*dst.SelectorExpr)
	if !ok {
		return
	}

	fields := stringFieldsOf(comp)

	switch sel.Sel.Name {
	case "SlackHelp":
		help.Type = "slack"
		help.SlackChannel = fields["Channel"]
		help.SlackTeam = fields["Team"]
	case "TeamsHelp":
		help.Type = "teams"
		help.TeamsChannel = fields["Channel"]
		help.TeamsTeam = fields["Team"]
	}
}

// extractTelemetryLiteral recovers a ManifestTelemetry from a
// props.TelemetryConfig{...} composite literal (Endpoint / OTelEndpoint).
func extractTelemetryLiteral(value dst.Expr, tel *ManifestTelemetry) {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return
	}

	fields := stringFieldsOf(comp)
	tel.Endpoint = fields["Endpoint"]
	tel.OTelEndpoint = fields["OTelEndpoint"]
}

// extractBootstrapLiteral recovers a ManifestBootstrap from a
// props.BootstrapPolicy{AutoInitialise: ..., SkipConfigCheck: [...]} literal.
func extractBootstrapLiteral(value dst.Expr, bs *ManifestBootstrap) {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "AutoInitialise":
			bs.AutoInitialise = boolLitValue(kv.Value)
		case "SkipConfigCheck":
			bs.SkipConfigCheck = stringSliceLitValue(kv.Value)
		case "AuxiliaryCommands":
			bs.AuxiliaryCommands = stringSliceLitValue(kv.Value)
		}
	}
}

// stringFieldsOf collects the string-valued keyed fields of a composite literal
// into a name->value map, for the small nested structs above.
func stringFieldsOf(comp *dst.CompositeLit) map[string]string {
	out := make(map[string]string, len(comp.Elts))

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			continue
		}

		if v, ok := stringLitValue(kv.Value); ok {
			out[key.Name] = v
		}
	}

	return out
}

// intLitValue returns the integer value of an int basic literal.
func intLitValue(expr dst.Expr) (int64, bool) {
	lit, ok := expr.(*dst.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}

	n, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		return 0, false
	}

	return n, true
}

// boolLitValue returns the value of a true/false identifier expression.
func boolLitValue(expr dst.Expr) bool {
	id, ok := expr.(*dst.Ident)

	return ok && id.Name == "true"
}

// stringSliceLitValue returns the string elements of a []string{...} composite
// literal (used for BootstrapPolicy.SkipConfigCheck).
func stringSliceLitValue(expr dst.Expr) []string {
	comp, ok := expr.(*dst.CompositeLit)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(comp.Elts))

	for _, elt := range comp.Elts {
		if v, ok := stringLitValue(elt); ok {
			out = append(out, v)
		}
	}

	return out
}

// extractConfigSpecLiteral recovers the layer set and own format from a
// props.ConfigSpec literal. The formats are the imports of config.go, read
// separately.
func extractConfigSpecLiteral(value dst.Expr, c *ManifestConfig) {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Layers":
			c.Layers = layersFromLiteral(kv.Value)
		case "Format":
			if v, ok := stringLitValue(kv.Value); ok {
				c.Format = v
			}
		}
	}
}

// layersFromLiteral reads []props.ConfigLayer{props.LayerDefaults, ...} back
// into manifest names.
func layersFromLiteral(value dst.Expr) []string {
	comp, ok := value.(*dst.CompositeLit)
	if !ok {
		return nil
	}

	var layers []string

	for _, elt := range comp.Elts {
		sel, ok := elt.(*dst.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "Layer") {
			continue
		}

		layers = append(layers, strings.ToLower(strings.TrimPrefix(sel.Sel.Name, "Layer")))
	}

	return layers
}
