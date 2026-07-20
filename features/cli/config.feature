@cli @integration
Feature: CLI Config Command
  The config command provides programmatic read/write access to individual
  configuration values, suitable for CI pipelines and scripted setup.
  Interactive reconfiguration of subsystems should use "init <subsystem>" instead.

  Background:
    Given the gtb binary is built
    And a temporary directory with a config file:
      """
      log:
        level: info
      """

  Scenario: Get a known configuration value
    When I run gtb with "config get log.level"
    Then the exit code is 0
    And stdout equals "info"

  Scenario: Get fails for an unknown key
    When I run gtb with "config get nonexistent.key"
    Then the exit code is not 0
    And stderr contains "nonexistent.key"

  Scenario: Set writes a value and get reads it back
    When I run gtb with "config set log.level debug"
    Then the exit code is 0
    When I run gtb with "config get log.level"
    Then the exit code is 0
    And stdout equals "debug"

  Scenario: List masks sensitive values
    Given the config file contains:
      """
      github:
        auth:
          token: supersecrettoken
      log:
        level: info
      """
    When I run gtb with "config list"
    Then the exit code is 0
    And stdout contains "log.level"
    And stdout contains "github.auth.token"
    And stdout does not contain "supersecrettoken"

  Scenario: Validate heals a missing required key from the embedded defaults
    Given a config file with no log.level key
    When I run gtb with "config validate"
    Then the exit code is 0
    And stdout contains "configuration is valid"

  Scenario: Validate reports error for an invalid value
    Given the config file contains:
      """
      log:
        level: verbose
      """
    When I run gtb with "config validate"
    Then the exit code is not 0
    And stdout contains "error:"
    And stdout contains "log.level"

  Scenario: Get outputs JSON with --output flag
    When I run gtb with "config get log.level --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "data.key" equals "log.level"
    And the JSON field "data.value" equals "info"

  Scenario: Unset removes a value and get can no longer read it
    When I run gtb with "config set feature.enabled true"
    Then the exit code is 0
    When I run gtb with "config unset feature.enabled"
    Then the exit code is 0
    And stdout contains "unset feature.enabled"
    When I run gtb with "config get feature.enabled"
    Then the exit code is not 0

  Scenario: Unset of a defaulted key falls back to the embedded default
    When I run gtb with "config set log.level debug"
    Then the exit code is 0
    When I run gtb with "config unset log.level"
    Then the exit code is 0
    And stdout contains "unset log.level"
    When I run gtb with "config get log.level"
    Then the exit code is 0
    And stdout contains "info"

  Scenario: Path prints contributing files and the writable target
    When I run gtb with "config path"
    Then the exit code is 0
    And stdout contains "config.yaml"
    And stdout contains "writable"

  Scenario: Path with --writable prints only the writable target
    When I run gtb with "config path --writable"
    Then the exit code is 0
    And stdout contains "config.yaml"
    And stdout does not contain "contributing"

  Scenario: Edit refuses to run without an interactive terminal
    When I run gtb with "config edit"
    Then the exit code is not 0
    And stderr contains "interactive terminal"
