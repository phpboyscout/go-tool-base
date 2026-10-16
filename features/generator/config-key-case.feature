@generator @integration
Feature: Config key case
  A generated tool folds config keys to lower case by default, so Log.Level and
  log.level are one key. A project whose keys must keep their case records
  properties.config.key_case: exact, which the generator renders into
  props.Tool.Config so every Store the built tool makes compares keys exactly.
  The mode survives regeneration and reconstructs from the code, and changing
  it warns, because it changes how the tool's users' existing files read.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0209-case-sensitive-config-keys (D8).

  Scenario: a fresh project says nothing about key case, which is folded
    Given a freshly generated gtb project
    Then the project manifest does not contain "key_case"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "KeyCase"

  Scenario: a project generated with exact keys renders the field
    Given I generate a gtb project with the flags "--config-key-case exact"
    Then the project manifest contains "key_case: exact"
    And the generated "pkg/cmd/root/cmd.go" file contains "KeyCase: props.KeyCaseExact"

  Scenario: set config.key_case exact renders the field, warns, and survives regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "set config.key_case exact"
    Then the project exit code is 0
    And the project output contains "keys are now case-sensitive"
    And the project manifest contains "key_case: exact"
    And the generated "pkg/cmd/root/cmd.go" file contains "KeyCase: props.KeyCaseExact"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the project output does not contain "keys are now"
    And the generated "pkg/cmd/root/cmd.go" file contains "KeyCase: props.KeyCaseExact"
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "key_case: exact"

  Scenario: set config.key_case folded returns to the default rendering, and warns
    Given I generate a gtb project with the flags "--config-key-case exact"
    When I run gtb in the project with "set config.key_case folded"
    Then the project exit code is 0
    And the project output contains "keys are now folded"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "KeyCase"

  Scenario: an unknown key case is refused and nothing changes
    Given a freshly generated gtb project
    When I run gtb in the project with "set config.key_case upper"
    Then the project exit code is not zero
    And the project manifest does not contain "key_case"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "KeyCase"
