@generator @integration
Feature: .gtb/ignore distinguishes regeneration from wiring
  A plain ignore rule stops the generator rewriting a file from source. It does
  not stop the localised edits that wire a subcommand into its parent, because
  the cost of refusing those lands on the program rather than the file: the
  command still compiles, it is simply absent from the built CLI. The `sealed`
  attribute forbids every write, and the run says what it could not do.

  Implements spec 0188.

  Scenario: A plain rule stops regeneration but still wires a new subcommand
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name parent --short parent-cmd"
    And I hand-edit the generated "pkg/cmd/parent/cmd.go" file
    And I run gtb in the project with "ignore add pkg/cmd/parent/cmd.go"
    And I run gtb in the project with "generate command --name child --parent parent --short child-cmd"
    Then the project exit code is 0
    And the generated "pkg/cmd/parent/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/parent/cmd.go" file contains "NewCmdChild"

  Scenario: A sealed rule blocks the wiring and names what it could not register
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name parent --short parent-cmd"
    And I hand-edit the generated "pkg/cmd/parent/cmd.go" file
    And I run gtb in the project with "ignore seal pkg/cmd/parent/cmd.go"
    Then the project output contains "sealed: pkg/cmd/parent/cmd.go"
    And the project output contains "v0.37.0"
    When I run gtb in the project with "generate command --name child --parent parent --short child-cmd"
    Then the project exit code is 0
    And the generated "pkg/cmd/parent/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/parent/cmd.go" file does not contain "NewCmdChild"
    And the project output contains "sealed, not wired"

  Scenario: ignore check reports the tier, not just a yes or no
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "ignore check pkg/cmd/alpha/cmd.go"
    Then the project output contains "managed"
    When I run gtb in the project with "ignore add pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "ignore check pkg/cmd/alpha/cmd.go"
    Then the project output contains "ignored"
    When I run gtb in the project with "ignore seal pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "ignore check pkg/cmd/alpha/cmd.go"
    Then the project output contains "sealed"

  Scenario: unseal drops back to ignored rather than handing the file back
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "ignore seal pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "ignore unseal pkg/cmd/alpha/cmd.go"
    Then the project exit code is 0
    And the project output contains "unsealed (still ignored)"
    When I run gtb in the project with "ignore check pkg/cmd/alpha/cmd.go"
    Then the project output contains "ignored"
    And the project output does not contain "sealed"

  Scenario: Un-ignoring a hand-edited file raises a conflict instead of overwriting it
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I hand-edit the generated "pkg/cmd/alpha/cmd.go" file
    And I run gtb in the project with "ignore add pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the project output does not contain "conflict detected"
    When I run gtb in the project with "ignore remove pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the project output contains "conflict detected"
    And the generated "pkg/cmd/alpha/cmd.go" file contains "hand-edited, do not clobber"
