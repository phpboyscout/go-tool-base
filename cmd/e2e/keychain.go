package main

// Mirror cmd/gtb/keychain.go so the E2E test binary has the same
// credentials Backend installed as the shipped product. Without this
// import the e2e binary would run with the stub backend and any
// Gherkin / manual scenario exercising keychain mode would fail to
// store, rendering those paths untestable.
import _ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
