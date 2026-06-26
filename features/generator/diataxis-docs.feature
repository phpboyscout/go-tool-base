@generator @integration
Feature: Diátaxis-structured documentation generation
  A freshly generated project scaffolds the neutral Diátaxis docs tree, records
  the diataxis layout in its manifest, and `generate docs` places generated pages
  in the correct quadrant (commands -> reference/cli, packages ->
  explanation/components) with leaf commands as flat files and parents as
  subsections.

  Scenario: a freshly generated project scaffolds the neutral Diátaxis tree
    Given a freshly generated gtb project
    Then the generated "docs/index.md" file exists
    And the generated "docs/getting-started.md" file exists
    And the generated "docs/how-to/index.md" file exists
    And the generated "docs/reference/index.md" file exists
    And the generated "docs/reference/cli/index.md" file exists
    And the generated "docs/reference/config/index.md" file exists
    And the generated "docs/explanation/index.md" file exists
    And the generated "docs/explanation/components/index.md" file exists
    And the generated "docs/explanation/concepts/index.md" file exists
    And the generated "docs/tutorials/index.md" file exists
    And the generated "docs/about/index.md" file does not exist
    And the project manifest contains "docs_layout: diataxis"

  Scenario: a generated leaf command lands in reference/cli, not the flat tree
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --agentless --short deploy-tool"
    Then the project exit code is 0
    And the generated "docs/reference/cli/deploy.md" file exists
    And the generated "docs/commands/deploy/index.md" file does not exist

  Scenario: a subcommand lands beside its parent under reference/cli
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --agentless --short deploy-tool"
    Then the project exit code is 0
    When I run gtb in the project with "generate command --name start --parent deploy --agentless --short start-it"
    Then the project exit code is 0
    And the generated "docs/reference/cli/deploy/start.md" file exists
