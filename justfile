set dotenv-load

# Tidy both modules and keep the workspace in step: sync lifts cli/'s pins to
# the workspace's resolution, and the GOWORK=off tidy restores the go.sum
# lines a `go install` of the CLI needs. CI's go-work-sync job runs the same
# pair and fails on a diff.
tidy:
    go mod tidy
    go work sync
    cd cli && GOWORK=off go mod tidy

# Apply go fix to update deprecated API usage
fix:
    go fix ./... ./cli/...

# Run go generate
generate:
    go generate ./... ./cli/...

# Build the gtb binary
[default]
build: tidy generate
    go build -o bin/gtb ./cli/cmd/gtb

# Generate roff man pages for gtb's command tree into ./man/man1
man: build
    ./bin/gtb generate man --dir man

# Build a snapshot release with goreleaser
snapshot:
    goreleaser build --snapshot --clean

# Run golangci-lint
lint:
    golangci-lint run ./... ./cli/...

# Run golangci-lint with auto-fix
lint-fix:
    golangci-lint run --fix ./... ./cli/...

# Run Go tests with coverage
test:
    go test ./... ./cli/... -v -cover

# Run Go tests with race detector
test-race:
    go test -race ./... ./cli/...

# Run integration tests
test-integration:
    INT_TEST=1 go test ./... ./cli/... -v

# Run E2E (Godog BDD) tests
test-e2e:
    INT_TEST_E2E=1 go test ./cli/test/e2e/... -v -timeout 15m

# Run E2E smoke tests only (fast, no external deps)
test-e2e-smoke:
    INT_TEST_E2E=1 INT_TEST_E2E_SMOKE=1 go test ./cli/test/e2e/... -v -timeout 2m

# Generate HTML coverage report and open it
coverage:
    go test ./... ./cli/... -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Generate coverage report including integration tests
coverage-full:
    INT_TEST=1 go test ./... ./cli/... -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Run benchmarks
bench:
    go test -bench=. -benchmem ./... ./cli/...

# Run pre-commit checks and documentation linting
check:
    pre-commit run --all-files
    ./scripts/lint-docs-errors.sh

# Regenerate all mocks
mocks:
    mockery

# Check for vulnerabilities in dependencies
vuln:
    govulncheck ./... ./cli/...

# Run Trivy filesystem scan
trivy:
    trivy fs --severity HIGH,CRITICAL --skip-dirs .claude .

# Run gitleaks secret scan
gitleaks:
    gitleaks detect --source . -v

# Run OSV dependency scanner
osv-scan:
    osv-scanner scan source -L go.mod

# Run all security scans
security: vuln trivy gitleaks osv-scan
    @echo "All security scans passed"

# Report public-API changes vs the latest release tag (advisory, pre-1.0)
apidiff *args:
    ./scripts/apidiff.sh {{args}}

# Advisory per-package ≥90% coverage check (flags sub-90 packages not excluded)
coverage-policy *args:
    ./scripts/coverage-policy.sh {{args}}

# Find unreachable exported symbols
deadcode:
    deadcode ./... ./cli/...

# Install the gtb binary to $GOPATH/bin
install:
    go install ./cli/cmd/gtb

# Serve documentation locally (pass ARGS, e.g. `just docs-serve "-a 0.0.0.0:8000"`)
docs-serve ARGS="":
    zensical serve {{ARGS}}

# Run the full local CI suite (mirrors GitHub Actions)
ci: tidy generate test test-race lint test-e2e
    @echo "CI suite passed"

# Cleanup build artifacts
[confirm]
cleanup:
    rm -rf bin
    rm -rf site
    rm -rf .cache
    rm -rf dist
    rm -f coverage.out coverage.html
