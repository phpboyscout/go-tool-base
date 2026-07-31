@cli @smoke
Feature: Fresh install auxiliary commands
  On a fresh install no config file exists yet. Cobra's own auxiliary
  commands — help, completion and the hidden __complete used by shell
  tab-completion — must work in that state instead of failing the
  missing-config bootstrap gate, and a genuinely config-gated command must
  fail with a hint that names "<tool> init". See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0145-bootstrap-auxiliary-command-exemptions.

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

  Scenario: a config-gated command fails with a run-init hint
    When I run gtb with "doctor"
    Then the exit code is not 0
    And stderr contains "no config file found"
    And stderr contains "gtb init"
