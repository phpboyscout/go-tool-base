@generator @integration
Feature: regenerate preserves nested subcommands
  `gtb regenerate manifest` must capture parent->child command nesting wired via
  the setup.Command middleware wrapper (cmd.Register(child...)), and
  `gtb regenerate project` must re-emit that registration — so regenerate does
  not silently destroy the command tree.

  Regression guard for the keryx "regenerate drops nested subcommands"
  data-loss bug (2026-06-16).

  Scenario: A Register-wired subcommand survives the regenerate round-trip
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name reel --short reels --agentless"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name build --parent reel --short build-a-reel --agentless"
    Then the project exit code is 0
    And the generated "pkg/cmd/reel/cmd.go" file contains "Register(build.NewCmdBuild"
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "name: build"
    When I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "pkg/cmd/reel/cmd.go" file contains "Register(build.NewCmdBuild"

  Scenario: A command group survives the round-trip with the framework default
    # Issue #21 reported a Run<Name> stub the generator wrote and never called.
    # It was first answered by wiring the RunE; spec 0190 answers it the other
    # way — a command with no run logic gets no run function, and its RunE is
    # setup.GroupRunE. What matters here is that a regenerate round-trip keeps
    # emitting that, rather than reverting to a call into main.go.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name parent --short parent-cmd"
    And I run gtb in the project with "generate command --name child --parent parent --short child-cmd"
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "pkg/cmd/parent/cmd.go" file contains "setup.GroupRunE"
    And the generated "pkg/cmd/parent/cmd.go" file does not contain "RunParent(cmd.Context()"
    # The child is a leaf, and a leaf is unaffected.
    And the generated "pkg/cmd/parent/child/main.go" file contains "errorhandling.ErrNotImplemented"
