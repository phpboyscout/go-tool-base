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
    And stdout contains "--rollback"
    And stdout contains "--pin"
    And stdout contains "--unpin"

  Scenario: Invalid semver format returns validation error
    When I run gtb with "update --version bad"
    Then the exit code is not 0
    And stderr contains "invalid version format"
    And stderr contains "expected a release tag such as v1.2.3"

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
    And stderr contains "nothing to update"
    And stderr does not contain "Update complete"
    When I run gtb with "update --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "data.updated" equals "false"

  Scenario: A release source reporting an older latest is refused without force
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "stale-latest"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "refusing to downgrade"

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

  # The static release channel (spec 0203): the binary discovers and retrieves
  # releases from a pointer and per-tag manifests at a plain HTTPS location,
  # with no forge involved. The step starts that location in the test process
  # and hands its URL to the binary, so the whole static branch runs
  # hermetically. The outcomes mirror the forge double's above.

  Scenario: On the static channel, already on the latest version is a no-op
    Given a static release channel serving "already-latest"
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "already running latest version"
    And stderr contains "nothing to update"

  Scenario: On the static channel, a pointer to an older release is refused without force
    Given a static release channel serving "stale-latest"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "refusing to downgrade"

  Scenario: On the static channel, a tag with no manifest fails clearly
    Given a static release channel serving "chain"
    When I run gtb with "update --version v9.9.9"
    Then the exit code is not 0
    And stderr contains "no release with that tag"
    And stderr contains "latest.json"

  Scenario: On the static channel, a corrupt checksum aborts the update before replacing the binary
    Given a static release channel serving "bad-checksum"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "checksum mismatch"

  Scenario: On the static channel, nothing published yet is named, not blamed on configuration
    Given a static release channel serving "nothing-published"
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "no release has been published"
    And stderr contains "not a configuration problem"

  # Pin and rollback (spec 0104). Only outcomes that leave the binary in place
  # run here; a successful rollback self-replaces and is covered Go-side.

  Scenario: An unattended rollback needs force
    Given a static release channel serving "rollback-chain"
    When I run gtb with "update --rollback"
    Then the exit code is not 0
    And stderr contains "rolling back from v1.0.0 to v0.9.0"
    And stderr contains "--force"

  Scenario: A rollback to a release with a corrupt checksum leaves the binary and writes no pin
    Given a static release channel serving "rollback-bad-checksum"
    When I run gtb with "update --rollback --force"
    Then the exit code is not 0
    And stderr contains "checksum mismatch"
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "already running latest version"
    And stderr does not contain "pinned"

  Scenario: A rollback target that is not older is refused
    Given a static release channel serving "chain"
    When I run gtb with "update --rollback=v1.1.0"
    Then the exit code is not 0
    And stderr contains "not a rollback"
    And stderr contains "--version v1.1.0"

  Scenario: A version given after the flag instead of attached to it is refused
    When I run gtb with "update --rollback v0.9.0"
    Then the exit code is not 0
    And stderr contains "--rollback=v0.9.0"

  Scenario: A pinned tool is not updated until it is unpinned
    Given a static release channel serving "chain"
    When I run gtb with "update --pin"
    Then the exit code is 0
    And stderr contains "pinned to v1.0.0"
    When I run gtb with "update --output json"
    Then the exit code is 0
    And the JSON field "data.updated" equals "false"
    And the JSON field "data.pinned" equals "v1.0.0"
    When I run gtb with "update"
    Then stderr contains "--unpin"
    And stderr does not contain "Update complete"
    When I run gtb with "update --unpin"
    Then the exit code is 0
    And stderr contains "unpinned"
