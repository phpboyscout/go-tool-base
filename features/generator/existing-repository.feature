@generator @integration
Feature: Generating into an existing repository
  An estate repository is created with its own files before a tool is
  scaffolded into it. A file gtb never created is the developer's: generate
  keeps it unless told otherwise, says so in its summary, and does not add a
  second Renovate config beside one the project already has (#113).

  Scenario: A hand-written file is kept under --overwrite deny
    Given an existing repository holding ".gitignore" with "/coverage.out"
    When I generate a gtb project with the flags "--overwrite deny"
    Then the project exit code is 0
    And the generated ".gitignore" file contains "/coverage.out"
    And the generated ".gitignore" file does not contain "Binaries for programs and plugins"
    And the project output contains "not created by gtb"

  Scenario: An existing Renovate config is not duplicated
    Given an existing repository holding "renovate.json" with "{}"
    When I generate a gtb project with the flags "--overwrite deny"
    Then the project exit code is 0
    And the generated "renovate.json5" file does not exist
    And the generated ".github/renovate.json5" file does not exist
    And the project output contains "Renovate is already configured by renovate.json"
