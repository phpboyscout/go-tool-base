@generator @integration
Feature: A generated GitLab project releases with its assets already attached
  On GitLab CI, goreleaser uploads the release's artefacts first, and colophon
  creates the release on the tag pipeline once every link it makes resolves,
  so a release never exists with nothing on it. GitHub keeps colophon creating
  the release at merge and goreleaser attaching the files, until colophon can
  attach files there.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0219-a-generated-gitlab-project-releases-with-its-assets-already-attached
  D1 to D5.

  Scenario: A GitLab project on the forge channel uploads under the tool's name, not the repository's
    Given I generate a gtb project with the flags "--forge-backend gitlab --host gitlab.com --repo acme/feattool-repo"
    Then the project exit code is 0
    And the generated ".gitlab-ci.yml" file contains "assets: true"
    And the generated ".gitlab-ci.yml" file contains 'asset_base: "$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/feattool/$COLOPHON_VERSION"'
    And the generated ".goreleaser.yaml" file contains "packages/generic/feattool/{{ .Version }}/{{ .ArtifactName }}"
    And the generated ".goreleaser.yaml" file contains "custom_artifact_name: true"
    And the generated ".gitlab-ci.yml" file does not contain "feattool-repo/$COLOPHON_VERSION"
    And the generated ".goreleaser.yaml" file does not contain "keep-existing"

  Scenario: A GitLab project on the static channel links the store
    Given I generate a gtb project with the flags "--forge-backend gitlab --host gitlab.com --release-channel static --release-base-url https://pkg.acme.dev/acme/feattool"
    Then the project exit code is 0
    And the generated ".gitlab-ci.yml" file contains 'asset_base: "https://pkg.acme.dev/acme/feattool/$CI_COMMIT_TAG"'
    And the generated ".goreleaser.yaml" file does not contain "uploads:"
    And the generated ".goreleaser.yaml" file does not contain "keep-existing"

  Scenario: A GitHub project keeps goreleaser attaching the files
    Given a freshly generated gtb project
    Then the generated ".goreleaser.yaml" file contains "mode: keep-existing"
    And the generated ".goreleaser.yaml" file does not contain "uploads:"

  Scenario: A regenerate straight after generate leaves the release files alone
    Given I generate a gtb project with forge backend "gitlab" on host "gitlab.com"
    Then the project exit code is 0
    When I remember the generated ".goreleaser.yaml" file
    And I remember the generated ".gitlab-ci.yml" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated ".goreleaser.yaml" file is unchanged
    And the generated ".gitlab-ci.yml" file is unchanged
