set dotenv-load

# Run go mod tidy
tidy:
    go mod tidy

# Apply go fix to update deprecated API usage
fix:
    go fix ./...

# Run go generate
generate:
    go generate ./...

# Build the gtb binary
[default]
build: tidy generate
    go build -o bin/gtb ./cmd/gtb

# Build a snapshot release with goreleaser
snapshot:
    goreleaser build --snapshot --clean

# Run golangci-lint
lint:
    golangci-lint run

# Run golangci-lint with auto-fix
lint-fix:
    golangci-lint run --fix

# Run Go tests with coverage
test:
    go test ./... -v -cover

# Run Go tests with race detector
test-race:
    go test -race ./...

# Run integration tests
test-integration:
    INT_TEST=1 go test ./... -v

# Run E2E (Godog BDD) tests
test-e2e:
    INT_TEST_E2E=1 go test ./test/e2e/... -v -timeout 5m

# Run E2E smoke tests only (fast, no external deps)
test-e2e-smoke:
    INT_TEST_E2E=1 INT_TEST_E2E_SMOKE=1 go test ./test/e2e/... -v -timeout 2m

# Generate HTML coverage report and open it
coverage:
    go test ./... -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Generate coverage report including integration tests
coverage-full:
    INT_TEST=1 go test ./... -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Run benchmarks
bench:
    go test -bench=. -benchmem ./...

# Run pre-commit checks
check:
    pre-commit run --all-files

# Regenerate all mocks
mocks:
    mockery

# Check for vulnerabilities in dependencies
vuln:
    govulncheck ./...

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

# Find unreachable exported symbols
deadcode:
    deadcode ./...

# Install the gtb binary to $GOPATH/bin
install:
    go install ./cmd/gtb

# Serve documentation locally
docs-serve:
    mkdocs serve

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
