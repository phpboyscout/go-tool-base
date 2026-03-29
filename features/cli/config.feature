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

  Scenario: Validate reports error for missing required key
    Given a config file with no log.level key
    When I run gtb with "config validate"
    Then the exit code is not 0
    And stdout contains "error:"
    And stdout contains "log.level"

  Scenario: Get outputs JSON with --output flag
    When I run gtb with "config get log.level --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "key" equals "log.level"
    And the JSON field "value" equals "info"
