@generator @integration
Feature: A generated tool can release on the static channel
  A self-updating tool reads its releases from its forge by default. It can
  instead read them from a static location: a pointer and per-tag manifests
  under one https URL, with no forge involved. The generator records the
  channel and its base URL in the manifest, renders the tool's release source
  as the type and the URL alone, and adds to the release configuration the
  pieces that publish the documents. A project that is not hosted on a forge
  can self-update this way and no other.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0203-the-static-release-channel
  D1, D5, D7 and D9.

  Scenario: A hosted project opts into the static channel
    Given I generate a gtb project with the flags "--release-channel static --release-base-url https://pkg.acme.dev/acme/feattool"
    Then the project exit code is 0
    And the project manifest contains "type: static"
    And the project manifest contains "base_url: https://pkg.acme.dev/acme/feattool"
    And the project manifest contains "backend: github"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.ReleaseSourceStatic"
    And the generated "pkg/cmd/root/cmd.go" file contains "https://pkg.acme.dev/acme/feattool"
    And the generated ".goreleaser.yaml" file contains "before_publish:"
    And the generated ".goreleaser.yaml" file contains "go tool releasemanifest --dist dist --base-url https://pkg.acme.dev/acme/feattool"
    And the generated ".goreleaser.yaml" file contains 'directory: "acme/feattool/{{ .Tag }}"'
    And the generated ".goreleaser.yaml" file contains "scripts/move-pointer.sh"
    And the generated "scripts/move-pointer.sh" file contains "If-Match"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go-tool-base/cmd/releasemanifest"

  Scenario: A project that is not hosted self-updates from the static channel
    Given I generate a gtb project with the flags "--no-forge --module example.internal/feattool --release-channel static --release-base-url https://pkg.acme.dev/feattool"
    Then the project exit code is 0
    And the project manifest contains "type: static"
    And the project manifest does not contain "backend:"
    And the generated ".goreleaser.yaml" file does not contain "force_token"
    And the generated ".goreleaser.yaml" file contains "disable: true"
    And the generated "cmd/feattool/forge.go" file does not exist

  Scenario: The forge channel renders none of the static channel's pieces
    Given a freshly generated gtb project
    Then the project manifest contains "type: github"
    And the project manifest does not contain "base_url"
    And the generated ".goreleaser.yaml" file does not contain "before_publish"
    And the generated ".goreleaser.yaml" file does not contain "publishers:"
    And the generated "scripts/move-pointer.sh" file does not exist
    And the generated "go.mod" file does not contain "cmd/releasemanifest"

  Scenario: The static channel needs its location
    Given I generate a gtb project with the flags "--release-channel static"
    Then the project exit code is not zero
    And the project output contains "--release-base-url"

  Scenario: A plain-http location is refused, not warned
    Given I generate a gtb project with the flags "--release-channel static --release-base-url http://pkg.acme.dev/acme/feattool"
    Then the project exit code is not zero
    And the project output contains "release base URL"

  Scenario: A project that is not hosted cannot take the forge channel
    Given I generate a gtb project with the flags "--no-forge --module example.internal/feattool --release-channel forge"
    Then the project exit code is not zero
    And the project output contains "static"

  Scenario: The withdrawn direct channel is refused by name
    Given I generate a gtb project with the flags "--release-channel direct"
    Then the project exit code is not zero
    And the project output contains "withdrawn"
