@generator @integration
Feature: Enable and disable release-signing verification end-to-end
  `gtb enable signing` and `gtb disable signing` flip consumer-side self-update
  signature verification on a generated project. Enabling scaffolds the
  internal/trustkeys embed package, emits pkg/cmd/root/signing.go, and wires the
  Signing field into the generated root command; disabling drops that wiring and
  the generated signing.go but deliberately keeps internal/trustkeys and any
  author-added *.asc keys. The manifest carries the decision, so it survives
  `gtb regenerate project`.

  The existing feature-toggle feature only asserts that `Signing:` lands in
  cmd.go on a single enable. These scenarios cover the full lifecycle —
  scaffold, survive-regenerate, tear-down (keeping trust anchors), and
  re-enable. Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0071-signing-generator-feature
  and https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0086-enable-disable-features.

  Scenario: Enabling signing scaffolds the trust package and wiring, and survives regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "enable signing --email release@acme.test"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "Signing:"
    And the generated "pkg/cmd/root/signing.go" file exists
    And the generated "internal/trustkeys/trustkeys.go" file exists
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "Signing:"
    And the generated "pkg/cmd/root/signing.go" file exists

  Scenario: Disabling signing drops the wiring and signing.go but keeps the trust anchors
    Given a freshly generated gtb project
    When I run gtb in the project with "enable signing --email release@acme.test"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/signing.go" file exists
    When I run gtb in the project with "disable signing"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "Signing:"
    And the generated "pkg/cmd/root/signing.go" file does not exist
    And the generated "internal/trustkeys/trustkeys.go" file exists

  Scenario: Re-enabling signing restores the wiring with the trust keys intact
    Given a freshly generated gtb project
    When I run gtb in the project with "enable signing --email release@acme.test"
    Then the project exit code is 0
    When I run gtb in the project with "disable signing"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/signing.go" file does not exist
    When I run gtb in the project with "enable signing"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "Signing:"
    And the generated "pkg/cmd/root/signing.go" file exists
