@cli @integration
Feature: Project-local config trust
  A project-local ".gtb.yaml" at a repository root can tune workflow settings,
  but its security-sensitive keys (self-update verification, telemetry consent,
  credentials) are IGNORED until the user explicitly trusts the file — so a
  hostile clone cannot silently downgrade security posture. Trusting the file
  unlocks those keys; workflow-tuning keys always apply.

  The project file may be in any format the binary links, and is filtered the
  same way whatever its format (spec 0204 D16, D17). The e2e binary links
  TOML.

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

  Scenario: A hostile clone's TOML project file is filtered the same way
    Given a project-local config file named ".gtb.toml" with:
      """
      [update]
      require_signature = false
      [telemetry]
      enabled = true
      [log]
      level = "debug"
      """
    When I run gtb in the project directory with "config get log.level"
    Then the exit code is 0
    And stdout equals "debug"
    And stderr contains "ignoring security-sensitive keys"

  Scenario: Trusting a TOML project file unlocks its security-sensitive keys
    Given a project-local config file named ".gtb.toml" with:
      """
      [telemetry]
      enabled = true
      """
    When I run gtb in the project directory with "config trust"
    Then the exit code is 0
    And stdout contains ".gtb.toml"
    When I run gtb in the project directory with "config get telemetry.enabled"
    Then the exit code is 0
    And stdout equals "true"

  Scenario: Two project files in one directory are refused, naming both
    Given a project-local config file named ".gtb.yaml" with:
      """
      log:
        level: debug
      """
    And a project-local config file named ".gtb.toml" with:
      """
      [log]
      level = "warn"
      """
    When I run gtb in the project directory with "config get log.level"
    Then the exit code is not 0
    And stderr contains ".gtb.yaml"
    And stderr contains ".gtb.toml"

  Scenario: Trusting a project file does not let it choose where configuration comes from
    Given a project-local config file with:
      """
      config:
        sources:
          team:
            address: https://consul.attacker.example
      log:
        level: debug
      """
    When I run gtb in the project directory with "config trust"
    Then the exit code is 0
    When I run gtb in the project directory with "config get log.level"
    Then the exit code is 0
    And stdout equals "debug"
    And stderr contains "trust does not admit where configuration comes from"
