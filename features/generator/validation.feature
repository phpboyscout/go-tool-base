@generator @integration @smoke
Feature: Generator Input Validation
  The project generator validates every user-supplied field before
  writing any files. Adversarial inputs fail fast with a clear,
  field-scoped error message rather than smuggling content into
  the rendered skeleton.

  Covers the input-validation half of
  docs/development/specs/2026-04-02-generator-template-escaping.md.

  Background:
    Given the gtb binary is built
    And a temporary init directory

  Scenario: Help flag shows usage
    When I run gtb with "generate project --help"
    Then the exit code is 0
    And stdout contains "project"

  Scenario: Uppercase name is rejected
    When I run gtb with "generate project --name BadName --repo github.com/myorg/mytool --path {init_dir}"
    Then the exit code is not 0
    And stderr contains "Name"

  Scenario: Name with traversal segment is rejected
    When I run gtb with "generate project --name ../evil --repo github.com/myorg/mytool --path {init_dir}"
    Then the exit code is not 0
    And stderr contains "Name"

  Scenario: Repo with traversal is rejected
    When I run gtb with "generate project --name mytool --repo ../foo --path {init_dir}"
    Then the exit code is not 0
    And stderr contains "Repo"

  Scenario: Repo missing org segment is rejected
    When I run gtb with "generate project --name mytool --repo github.com --path {init_dir}"
    Then the exit code is not 0
    And stderr contains "Repo"

  Scenario: Invalid EnvPrefix is rejected
    When I run gtb with "generate project --name mytool --repo github.com/myorg/mytool --env-prefix lowercase --path {init_dir}"
    Then the exit code is not 0
    And stderr contains "EnvPrefix"
