@cli @smoke
Feature: Environment variable prefix
  Environment variables are scoped with a tool-specific prefix to prevent
  config pollution in shared environments.

  Background:
    Given the gtb binary is built

  Scenario: Prefixed environment variable overrides config
    When I set environment variable "GTB_LOG_LEVEL" to "debug"
    And I run gtb with "version"
    Then the exit code is 0

  Scenario: Unprefixed environment variable does not override config
    When I set environment variable "LOG_LEVEL" to "debug"
    And I run gtb with "version"
    Then the exit code is 0
