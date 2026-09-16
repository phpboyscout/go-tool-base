// Package features is the feature core: what a binary offers (declared at
// init into a Registry), what a tool has chosen (a Set, resolved once from an
// immutable Snapshot and the tool's states), and how a request asks (an
// Evaluator). It imports nothing of GTB so it can leave for go/features as a
// move (spec 0199 D2, D9).
//
// Declaration is process-wide because a blank import can only reach package
// state; everything after main works with values. A reader takes a Snapshot
// and is never surprised by a later registration, which is what the seals
// this package replaces used to panic about (D1).
package features
