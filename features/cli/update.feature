@cli @smoke
Feature: CLI Update Command
  The update command manages self-updating to newer versions with
  input validation and helpful usage information.

  Background:
    Given the gtb binary is built

  Scenario: Help flag shows usage and available flags
    When I run gtb with "update --help"
    Then the exit code is 0
    And stdout contains "Update the running binary to the latest version"
    And stdout contains "--force"
    And stdout contains "--version"

  Scenario: Invalid semver format returns validation error
    When I run gtb with "update --version bad"
    Then the exit code is not 0
    And stderr contains "invalid version format"
    And stderr contains "expected semVer pattern v0.0.0"

  Scenario: Empty version string with valid format is accepted
    When I run gtb with "update --version v999.999.999"
    Then the exit code is not 0
    And stderr does not contain "invalid version format"

  # The following scenarios drive an in-memory stub release source
  # (pkg/vcs/release/releasetest) injected via props.Tool.ReleaseProvider, so the
  # full self-update outcome is exercised hermetically — no network. They cover
  # only outcomes that abort BEFORE the binary is replaced; the happy "newer
  # version applies" path self-replaces the binary and is covered Go-side in
  # pkg/setup/update_e2e_test.go.

  Scenario: Already on the latest version is a no-op
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "already-latest"
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "already running latest version"

  Scenario: A requested version that does not exist fails clearly
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "not-found"
    When I run gtb with "update --version v9.9.9"
    Then the exit code is not 0
    And stderr contains "release not found"

  Scenario: A corrupt checksum aborts the update before replacing the binary
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "bad-checksum"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "checksum mismatch"

  Scenario: A bad signature aborts the update before replacing the binary
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "bad-signature"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "signature"
