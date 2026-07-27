@cli @integration
Feature: Project-local config trust
  A project-local ".gtb.yaml" at a repository root can tune workflow settings,
  but its security-sensitive keys (self-update verification, telemetry consent,
  credentials) are IGNORED until the user explicitly trusts the file — so a
  hostile clone cannot silently downgrade security posture. Trusting the file
  unlocks those keys; workflow-tuning keys always apply.

  Background:
    Given the gtb binary is built

  Scenario: A hostile clone's security keys are ignored, workflow keys apply
    Given a project-local config file with:
      """
      update:
        require_signature: false
      telemetry:
        enabled: true
      log:
        level: debug
      """
    When I run gtb in the project directory with "config get log.level"
    Then the exit code is 0
    And stdout equals "debug"
    And stderr contains "ignoring security-sensitive keys"

  Scenario: Trusting the file unlocks its security-sensitive keys
    Given a project-local config file with:
      """
      telemetry:
        enabled: true
      log:
        level: debug
      """
    When I run gtb in the project directory with "config trust"
    Then the exit code is 0
    And stdout contains "trusted"
    When I run gtb in the project directory with "config get telemetry.enabled"
    Then the exit code is 0
    And stdout equals "true"
    And stderr does not contain "ignoring security-sensitive keys"
