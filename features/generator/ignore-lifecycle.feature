@generator @integration
Feature: gtb ignore manages .gtb/ignore rules
  A generated project can mark deliberately-diverged generated files hands-off
  so `regenerate` stops re-rendering them and stops raising conflicts. The
  `gtb ignore` command group manages the .gtb/ignore file: add, remove, list,
  and check rules. A fresh scaffold ships a commented, inert .gtb/ignore so the
  mechanism is discoverable.

  Tracks GitLab issue #3.

  Scenario: A fresh scaffold ships a commented, discoverable .gtb/ignore
    Given a freshly generated gtb project
    Then the generated ".gtb/ignore" file exists
    And the generated ".gtb/ignore" file contains "gtb ignore add"
    And the generated ".gtb/ignore" file contains "regenerate"

  Scenario: ignore add creates a rule and is idempotent
    Given a freshly generated gtb project
    When I run gtb in the project with "ignore add justfile"
    Then the project exit code is 0
    And the project output contains "added: justfile"
    And the generated ".gtb/ignore" file contains "justfile"
    When I run gtb in the project with "ignore add justfile"
    Then the project exit code is 0
    And the project output contains "already present (no-op): justfile"

  Scenario: ignore check names the winning rule under negation
    Given a freshly generated gtb project
    When I run gtb in the project with "ignore add .github/workflows/**"
    And I run gtb in the project with "ignore check .github/workflows/test.yml"
    Then the project exit code is 0
    And the project output contains "ignored"
    And the project output contains ".github/workflows/**"

  Scenario: ignore remove drops the literal rule line
    Given a freshly generated gtb project
    When I run gtb in the project with "ignore add Dockerfile"
    Then the generated ".gtb/ignore" file contains "Dockerfile"
    When I run gtb in the project with "ignore remove Dockerfile"
    Then the project exit code is 0
    And the project output contains "removed: Dockerfile"
    And the generated ".gtb/ignore" file does not contain "Dockerfile"

  Scenario: ignore remove errors on an absent rule
    Given a freshly generated gtb project
    When I run gtb in the project with "ignore remove never-added"
    Then the project exit code is not zero

  Scenario: ignore list resolves rules against the manifest
    Given a freshly generated gtb project
    When I run gtb in the project with "ignore add justfile"
    And I run gtb in the project with "ignore list"
    Then the project exit code is 0
    And the project output contains "justfile"
    And the project output contains "ignored"
