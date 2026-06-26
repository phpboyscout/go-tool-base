@generator @integration
Feature: remove command and regenerate preserve a buildable, faithful project
  Regression guards for the keryx v0.19.0 report:
   - remove command must drop BOTH the import and the registration call;
   - regenerate manifest must preserve command Short/Long descriptions;
   - subcommand docs must be keyed by the full command path, not the leaf name.

  Scenario: remove command leaves no dangling registration in the root
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name foo --agentless --short widget"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "foo.NewCmdFoo"
    When I run gtb in the project with "remove command --name foo"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "foo.NewCmdFoo"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "pkg/cmd/foo"

  Scenario: regenerate manifest preserves command descriptions
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name social --agentless --short social-media-tools"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "description: social-media-tools"

  Scenario: same-named subcommands under different parents each get their own doc
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name a --agentless --short parent-a"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name b --agentless --short parent-b"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name run --parent a --agentless --short a-run"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name run --parent b --agentless --short b-run"
    Then the project exit code is 0
    And the generated "docs/reference/cli/a/run.md" file contains "run"
    And the generated "docs/reference/cli/b/run.md" file contains "run"
