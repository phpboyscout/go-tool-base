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
