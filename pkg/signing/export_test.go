package signing

// ResetForTesting is the test-only entry point for clearing the
// registry between test cases. Exported via this _test.go file so
// production code outside the package cannot reach it.
func ResetForTesting() { resetForTesting() }
