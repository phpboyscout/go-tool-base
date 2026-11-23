@generator @integration
Feature: Commands named after Go reserved words
  A command may be called import, type, go or any other Go keyword. Its Go
  package takes a cmd suffix, since `package import` cannot compile, and
  everything a user sees keeps the name they chose. See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0216-a-command-may-be-named-after-a-go-reserved-word.

  A command named after a predeclared identifier or a name its parent's file
  already uses keeps its package name, and the parent imports it under a cmd
  suffix instead (#189).

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
    And the project output contains "imported as typecmd"
    And the generated "pkg/cmd/typecmd/cmd.go" file does not exist

  Scenario: a command named after something its parent uses is imported under an alias
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name error --short an-error"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name handover --short hand-over"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name cobra --parent handover --short a-snake"
    Then the project exit code is 0
    And the generated "pkg/cmd/error/cmd.go" file contains "package error"
    And the generated "pkg/cmd/root/cmd.go" file contains "errorcmd.NewCmdError("
    And the generated "pkg/cmd/handover/cobra/cmd.go" file contains "package cobra"
    And the generated "pkg/cmd/handover/cmd.go" file contains "cobracmd.NewCmdCobra("
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "errorcmd.NewCmdError("
