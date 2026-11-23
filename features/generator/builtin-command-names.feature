@generator @integration
Feature: Top-level commands and the built-ins' names
  A built-in and a project's own top-level command can't share a name, because
  only one of them could run (#224). Disabling the feature that holds a name
  frees it, and a nested command may have any name. See
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0216-a-command-may-be-named-after-a-go-reserved-word.

  Scenario: a command named after an enabled feature is refused until it is disabled
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name update --short my-own"
    Then the project exit code is not zero
    And the project output contains "gtb disable update"
    And the generated "pkg/cmd/update/cmd.go" file does not exist
    When I run gtb in the project with "disable update"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name update --short my-own"
    Then the project exit code is 0
    And the generated "pkg/cmd/update/cmd.go" file exists
    When I run gtb in the project with "enable update"
    Then the project exit code is not zero
    And the project output contains "before enabling the update feature"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Disable(props.UpdateCmd)"

  Scenario: an alias follows the rule a name does
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name apply --alias update --short my-own"
    Then the project exit code is not zero
    And the project output contains "gtb disable update"
    And the generated "pkg/cmd/apply/cmd.go" file does not exist
    When I run gtb in the project with "disable update"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name apply --alias update --short my-own"
    Then the project exit code is 0
    When I run gtb in the project with "enable update"
    Then the project exit code is not zero
    And the project output contains "apply command has the alias update"
    When I run gtb in the project with "generate command --name apply --short my-own --force"
    Then the project exit code is 0
    When I run gtb in the project with "enable update"
    Then the project exit code is not zero
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Disable(props.UpdateCmd)"
    And the project output contains "run gtb regenerate project"
    When I remove the line "- update" from the project manifest
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/apply/cmd.go" file does not contain "Aliases"
    When I run gtb in the project with "enable update"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "props.Disable(props.UpdateCmd)"

  Scenario: version and help are always refused, and completion is allowed
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name version --short my-own"
    Then the project exit code is not zero
    And the project output contains "every tool has a version command"
    When I run gtb in the project with "generate command --name help --short my-own"
    Then the project exit code is not zero
    And the project output contains "every tool has a help command"
    When I run gtb in the project with "generate command --name completion --short my-own"
    Then the project exit code is 0

  Scenario: a nested command may have a built-in's name
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name server --short serve-it"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name update --parent server --short my-own"
    Then the project exit code is 0
    And the generated "pkg/cmd/server/update/cmd.go" file exists
