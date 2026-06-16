@generator @integration @smoke
Feature: Custom template overlays CLI surface
  The generator can layer custom template overlays from a local folder
  or a git repo over the embedded skeleton, and a `gtb template` command
  group manages the sources recorded in the manifest. A tampered or
  unsafe source spec is rejected at the CLI boundary before any fetch or
  write.

  Covers the CLI half of
  docs/development/specs/2026-06-15-generator-custom-partial-templates.md.

  Background:
    Given the gtb generator binary is built
    And a temporary init directory

  Scenario: generate project exposes the --template flag
    When I run gtb with "generate project --help"
    Then the exit code is 0
    And stdout contains "--template"

  Scenario: the template command group lists its subcommands
    When I run gtb with "template --help"
    Then the exit code is 0
    And stdout contains "add"
    And stdout contains "update"
    And stdout contains "remove"
    And stdout contains "list"

  Scenario: a traversal template location is rejected before any fetch
    When I run gtb with "generate project --name mytool --repo github.com/myorg/mytool --path {init_dir} --template ../../etc"
    Then the exit code is not 0
    And stderr contains "TemplateSourceLocation"

  Scenario: a plain-http git template source is refused
    When I run gtb with "generate project --name mytool --repo github.com/myorg/mytool --path {init_dir} --template http://example.com/acme/templates"
    Then the exit code is not 0
    And stderr contains "TemplateSourceLocation"

  Scenario: a non-existent local template source errors clearly
    When I run gtb with "generate project --name mytool --repo github.com/myorg/mytool --path {init_dir} --template ./does-not-exist"
    Then the exit code is not 0
    And stderr contains "does not exist"
