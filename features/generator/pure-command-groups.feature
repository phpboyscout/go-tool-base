@generator @integration
Feature: A command group with no run logic gets the framework default
  A command that only groups subcommands has nothing to implement. Its cmd.go
  wires setup.GroupRunE — usage and success when invoked bare, a named error on a
  verb it does not have — and no Run<Name> is generated for it at all.

  What that removes is a reference into the developer's own package. A cmd.go
  calling Run<Name> could dangle where a seal forbade creating the callee, and
  suppressing the call to keep it compiling made a built tool's exit code depend
  on .gtb/ignore. With nothing referenced, the ignore file governs what is
  written and nothing else.

  Implements spec 0190.

  Scenario: A group gets the framework default and its leaf-era stub is left alone
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    Then the generated "pkg/cmd/alpha/main.go" file contains "ErrNotImplemented"
    When I run gtb in the project with "generate command --name beta --parent alpha --short beta-cmd"
    Then the project exit code is 0
    And the generated "pkg/cmd/alpha/cmd.go" file contains "setup.GroupRunE"
    And the generated "pkg/cmd/alpha/cmd.go" file does not contain "RunAlpha(cmd.Context()"
    # D3: main.go belongs to the developer. The stub it still holds is a live
    # seam — give it a body and the group is working again — not litter for the
    # generator to tidy on their behalf.
    And the generated "pkg/cmd/alpha/main.go" file contains "ErrNotImplemented"

  Scenario: A sealed and absent main.go changes nothing about what is emitted
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "generate command --name beta --parent alpha --short beta-cmd"
    And I run gtb in the project with "ignore seal pkg/cmd/alpha/main.go"
    And I delete the generated "pkg/cmd/alpha/main.go" file
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    # This is keryx's configuration, and the one that used to emit a call to a
    # function the seal forbade creating: `undefined: RunAlpha`.
    And the generated "pkg/cmd/alpha/cmd.go" file contains "setup.GroupRunE"
    And the generated "pkg/cmd/alpha/cmd.go" file does not contain "RunAlpha(cmd.Context()"
    And the generated "pkg/cmd/alpha/main.go" file does not exist

  Scenario: A group with hand-written run logic keeps calling it
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I give the generated "alpha" command a hand-written run body
    And I run gtb in the project with "generate command --name beta --parent alpha --short beta-cmd"
    Then the project exit code is 0
    And the generated "pkg/cmd/alpha/cmd.go" file contains "RunAlpha(cmd.Context()"
    And the generated "pkg/cmd/alpha/cmd.go" file does not contain "setup.GroupRunE"
    And the generated "pkg/cmd/alpha/main.go" file contains "my own implementation"

  Scenario: The regeneration that changes a group's behaviour says so
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "generate command --name beta --parent alpha --short beta-cmd"
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    # Already on the new model, so there is no transition to report. A summary
    # line that fires on every run is one people learn to scroll past.
    And the project output does not contain "changed behaviour"
