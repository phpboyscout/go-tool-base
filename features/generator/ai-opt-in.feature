@generator @integration @ai-opt-in
Feature: gtb calls a model only when asked
  A plain generate spends no money. A model is called only when the run passes
  --ai (or --prompt, --script, or regenerate --update-docs), when the project's
  manifest records generator.ai, or when the person said yes in gtb init. A
  provider named on its own is not an ask (spec 0211, #137).

  Scenario: --provider on its own calls no model and says how to ask
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --short deploy-tool --provider claude"
    Then the project exit code is 0
    And the project output contains "pass --ai"
    And the project output does not contain "generating documentation with AI"
    And the generated "docs/reference/cli/deploy.md" file exists

  Scenario: a run cannot ask for a model and refuse one
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --short deploy-tool --prompt list-files --no-ai"
    Then the project exit code is not zero
    And the project output contains "--no-ai cannot be combined"

  Scenario: the project records its choice in the manifest
    Given a freshly generated gtb project
    When I run gtb in the project with "set generator.ai true"
    Then the project exit code is 0
    And the project manifest contains "generator:"
    And the project manifest contains "ai: true"

  Scenario: --agentless is gone
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name deploy --short deploy-tool --agentless"
    Then the project exit code is not zero
    And the project output contains "unknown flag: --agentless"
