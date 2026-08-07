@generator @integration
Feature: regenerate project completes on a project with hand-modified files
  A project whose generated files have been hand-edited can still be
  regenerated. Files the developer has declared theirs — by a .gtb/ignore rule
  or by declining the prompt — are left alone, and everything else regenerates.
  The run finishes and says what it left.

  Tracks GitLab issue #13.

  Scenario: A hand-edited command file is kept and the run still completes
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "generate command --name beta --short beta-cmd"
    And I hand-edit the generated "pkg/cmd/alpha/cmd.go" file
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/alpha/cmd.go" file contains "hand-edited, do not clobber"
    And the generated "pkg/cmd/beta/cmd.go" file contains "NewCmdBeta"
    And the project output contains "kept your version"
    And the project output contains "gtb ignore add pkg/cmd/alpha/cmd.go"

  Scenario: An ignore rule suppresses the conflict entirely
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I hand-edit the generated "pkg/cmd/alpha/cmd.go" file
    And I run gtb in the project with "ignore add pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/alpha/cmd.go" file contains "hand-edited, do not clobber"
    And the project output does not contain "conflict detected"

  Scenario: --overwrite allow reaches command files
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I hand-edit the generated "pkg/cmd/alpha/cmd.go" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "pkg/cmd/alpha/cmd.go" file does not contain "hand-edited, do not clobber"

  Scenario: doctor reports a diverged command file, and stops once it is declared
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I hand-edit the generated "pkg/cmd/alpha/cmd.go" file
    And I run gtb in the project with "doctor"
    Then the project output contains "pkg/cmd/alpha/cmd.go"
    When I run gtb in the project with "ignore add pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "doctor"
    Then the project output does not contain "pkg/cmd/alpha/cmd.go"

  Scenario: ignore list and ignore check agree about a command file
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name alpha --short alpha-cmd"
    And I run gtb in the project with "ignore add pkg/cmd/alpha/cmd.go"
    And I run gtb in the project with "ignore check pkg/cmd/alpha/cmd.go"
    Then the project output contains "ignored"
    When I run gtb in the project with "ignore list"
    Then the project output contains "pkg/cmd/alpha/cmd.go"
    And the project output does not contain "stale rule"
