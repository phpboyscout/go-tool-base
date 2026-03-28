@cli @integration
Feature: CLI Init Command
  The init command bootstraps configuration in a target directory,
  supporting non-interactive mode via skip flags and config merging.

  Background:
    Given the gtb binary is built
    And a temporary init directory

  Scenario: Non-interactive init creates config and gitignore
    When I run gtb with "init --skip-login --skip-key --skip-ai --dir {init_dir}"
    Then the exit code is 0
    And the file "config.yaml" exists in the init directory
    And the file ".gitignore" exists in the init directory

  Scenario: JSON output returns config path
    When I run gtb with "init --skip-login --skip-key --skip-ai --dir {init_dir} --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "status" equals "success"
    And the JSON field "command" equals "init"
    And the JSON field "data.config_path" is not empty

  Scenario: Init merges with existing config preserving user values
    Given the init directory contains a config file:
      """
      log:
        level: debug
      custom:
        key: preserved
      """
    When I run gtb with "init --skip-login --skip-key --skip-ai --dir {init_dir}"
    Then the exit code is 0
    And stderr contains "attempting to merge"
    And the config file in the init directory contains "custom"
    And the config file in the init directory contains "preserved"
    And the config file in the init directory contains "level: debug"

  Scenario: Clean init replaces existing config with defaults
    Given the init directory contains a config file:
      """
      custom:
        key: should-be-removed
      """
    When I run gtb with "init --skip-login --skip-key --skip-ai --dir {init_dir} --clean"
    Then the exit code is 0
    And the config file in the init directory does not contain "should-be-removed"
    And the config file in the init directory contains "log"

  Scenario: Help flag shows init usage and flags
    When I run gtb with "init --help"
    Then the exit code is 0
    And stdout contains "Initialises the default configuration"
    And stdout contains "--skip-login"
    And stdout contains "--skip-key"
    And stdout contains "--clean"
    And stdout contains "--dir"
