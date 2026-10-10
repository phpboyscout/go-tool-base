@cli @smoke
Feature: Fresh install auxiliary commands
  On a fresh install no config file exists yet. Cobra's own auxiliary
  commands — help, completion and the hidden __complete used by shell
  tab-completion — must work in that state instead of failing the
  missing-config bootstrap gate. See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0145-bootstrap-auxiliary-command-exemptions.
  Under --ci nobody can run init, so a config-reading command starts on the
  defaults; the "<tool> init" hint is for a person at a terminal, which the
  harness cannot provide, and is covered by the root's unit tests (spec 0208).

  Background:
    Given the gtb binary is built
    And an empty config directory

  Scenario: help prints usage with no config file
    When I run gtb with "help"
    Then the exit code is 0
    And stdout contains "Available Commands"

  Scenario: completion emits the bash script with no config file
    When I run gtb with "completion bash"
    Then the exit code is 0
    And stdout contains "bash completion"

  Scenario: shell tab-completion works with no config file
    # Bare invocation: __complete treats every trailing arg as the command
    # line being completed, so the harness must not append --ci/--config.
    # HOME is still isolated to the scenario's temp directory, so no config
    # file is found on the default paths — the fresh-install state.
    When I run gtb bare with "__complete ver"
    Then the exit code is 0
    And stdout contains "version"

  Scenario: under --ci a config-reading command starts on the defaults
    When I run gtb with "config list"
    Then the exit code is 0
    And stderr contains "no config file found; starting on the defaults"
    And stderr does not contain "gtb init"

  Scenario: doctor is not config-gated and diagnoses the fresh install
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "no config file yet"
