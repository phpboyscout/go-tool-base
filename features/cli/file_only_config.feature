@cli @integration
Feature: File-only config keys
  A tool can declare config keys that no prefixed variable sets, so a
  variable inherited from a job's environment cannot loosen them. The e2e
  binary declares update.** file-only (spec 0214).

  Background:
    Given the gtb binary is built
    And a temporary directory with a config file:
      """
      log:
        level: info
      update:
        channel: stable
      """

  Scenario: A variable aimed at a file-only key is ignored with a warning
    Given I set environment variable "GTB_UPDATE_CHANNEL" to "do-not-print"
    When I run gtb with "config get update.channel"
    Then the exit code is 0
    And stdout equals "stable"
    And stderr contains "file-only"
    And stderr contains "GTB_UPDATE_CHANNEL"
    And stderr does not contain "do-not-print"

  Scenario: A bare section variable cannot replace the section
    Given I set environment variable "GTB_UPDATE" to "x"
    When I run gtb with "config get update.channel"
    Then the exit code is 0
    And stdout equals "stable"
    And stderr contains "GTB_UPDATE"

  Scenario: A key outside the list still takes the environment
    Given I set environment variable "GTB_LOG_LEVEL" to "debug"
    When I run gtb with "config get log.level"
    Then the exit code is 0
    And stdout equals "debug"

  Scenario: doctor lists the file-only keys and names the ignored variable
    Given I set environment variable "GTB_UPDATE_CHANNEL" to "do-not-print"
    When I run gtb with "doctor --ci"
    Then the exit code is 0
    And stdout contains "[!!] File-only config keys"
    And stdout contains "update.**"
    And stdout contains "GTB_UPDATE_CHANNEL"
    And stdout does not contain "do-not-print"
