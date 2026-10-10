@cli @smoke
Feature: An unattended run starts without a person
  A run with nobody at the terminal (a service under systemd, a pod, cron, a
  script) is never stopped by a question nobody can answer. The harness gives
  the binary a piped stdin, so these runs are unattended without --ci; CI is
  cleared because the runners export it. See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0208-an-unattended-run-starts-without-a-person.

  Background:
    Given the gtb binary is built
    And I set environment variable "CI" to ""

  Scenario: the enabled update policy does not block an unattended run
    Given a temporary directory with a config file:
      """
      log:
        level: info
      update:
        policy: enabled
      """
    And I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "newer-available"
    When I run gtb unattended with "config list"
    Then the exit code is 0
    And stderr contains "not enforced without a terminal"
    And stderr does not contain "is required before continuing"

  Scenario: an unattended run with no config file starts on the defaults
    Given an empty config directory
    When I run gtb bare with "config list"
    Then the exit code is 0
    And stderr contains "no config file found; starting on the defaults"
