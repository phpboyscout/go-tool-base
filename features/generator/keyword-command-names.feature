@generator @integration
Feature: Commands named after Go reserved words
  A command may be called import, type, go or any other Go keyword. Its Go
  package takes a cmd suffix, since `package import` cannot compile, and
  everything a user sees keeps the name they chose. See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0216-a-command-may-be-named-after-a-go-reserved-word.

  Scenario: a keyword-named subcommand keeps its name everywhere but its package
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name handover --short hand-over"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name import --parent handover --short import-it"
    Then the project exit code is 0
    And the generated "pkg/cmd/handover/import/cmd.go" file contains "package importcmd"
    And the generated "pkg/cmd/handover/import/cmd.go" file contains 'Use:   "import"'
    And the generated "pkg/cmd/handover/cmd.go" file contains "importcmd.NewCmdImport("
    And the generated "docs/reference/cli/handover/import.md" file exists

  Scenario: a command whose package a sibling already has is refused
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name type --short a-type"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name typecmd --short another"
    Then the project exit code is not zero
    And the project output contains "package typecmd"
    And the generated "pkg/cmd/typecmd/cmd.go" file does not exist
