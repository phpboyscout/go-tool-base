// Package verifier provides post-generation verification strategies that validate
// generated projects compile and pass tests. The [AgentVerifier] uses an AI-powered
// autonomous repair loop — invoking Go build, test, and lint tools iteratively
// until the project is healthy or a retry limit is reached.
package verifier
