@generator @integration @homebrew-tap
Feature: A generated tool can publish itself to a Homebrew tap
  A project names a tap on its own GitLab or GitHub, and each release then
  renders a cask and publishes it there. On GitLab the pipeline pushes it with
  the CI job token through a scaffolded script; on GitHub GoReleaser pushes it
  with a tap-token secret. A project that names no tap renders none of it.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0212-a-generated-tool-can-publish-to-a-homebrew-tap
  D2, D3, D5 and D7.

  Scenario: A GitLab project names a tap, then drops it
    Given I generate a gtb project with the flags "--forge-backend gitlab --homebrew-tap acme/homebrew"
    Then the project exit code is 0
    And the project manifest contains "tap: acme/homebrew"
    And the generated ".goreleaser.yaml" file contains "homebrew_casks:"
    And the generated ".goreleaser.yaml" file contains "skip_upload: true"
    And the generated ".gitlab-ci.yml" file contains "homebrew-tap:"
    And the generated ".gitlab-ci.yml" file contains "bash scripts/push-homebrew-cask.sh"
    And the generated "scripts/push-homebrew-cask.sh" file contains "CI_JOB_TOKEN"
    When I run gtb in the project with "unset homebrew.tap"
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the project manifest does not contain "homebrew"
    And the generated ".goreleaser.yaml" file does not contain "homebrew_casks"
    And the generated ".gitlab-ci.yml" file does not contain "homebrew-tap"
    And the generated "scripts/push-homebrew-cask.sh" file does not exist

  Scenario: A GitHub project gains a tap later
    Given a freshly generated gtb project
    Then the generated ".goreleaser.yaml" file does not contain "homebrew_casks"
    When I run gtb in the project with "set homebrew.tap acme/homebrew-tap"
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated ".goreleaser.yaml" file contains "skip_upload: auto"
    And the generated ".goreleaser.yaml" file contains "HOMEBREW_TAP_TOKEN"
    And the generated ".github/workflows/goreleaser.yaml" file contains "secrets.HOMEBREW_TAP_TOKEN"
    And the generated "scripts/push-homebrew-cask.sh" file does not exist

  Scenario: A project that is not hosted has no tap to push to
    Given I generate a gtb project with the flags "--no-forge --module example.internal/feattool --release-channel static --release-base-url https://pkg.acme.dev/feattool --homebrew-tap acme/homebrew"
    Then the project exit code is not zero
    And the project output contains "GitLab or GitHub"
