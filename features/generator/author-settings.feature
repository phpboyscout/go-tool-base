@generator @integration
Feature: The manifest owns every author setting
  An author's settings arrive through flags or the wizard, are recorded in
  .gtb/manifest.yaml, and are read back by regenerate unchanged. A setting that
  only the manifest knew, or that regenerate recomputed from the author's
  machine, is one the author cannot rely on.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0197-author-settings-as-one-surface
  D3, D4, D7, D8 and D9.

  Scenario: The Go version is recorded and regenerate leaves the go line alone
    Given a freshly generated gtb project
    Then the project manifest contains "go: "
    When I remember the generated "go.mod" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "go.mod" file is unchanged

  Scenario: Telemetry endpoints, bootstrap posture and enforcement have flags and survive regenerate
    Given I generate a gtb project with the flags "--features update,init,telemetry --telemetry-endpoint https://t.example.internal --auto-initialise --skip-config-check version --auxiliary-commands completion --signing --signing-require-checksum"
    Then the project exit code is 0
    And the project manifest contains "endpoint: https://t.example.internal"
    And the project manifest contains "auto_initialise: true"
    And the project manifest contains "auxiliary_commands:"
    And the project manifest contains "require_checksum: true"
    And the generated "pkg/cmd/root/cmd.go" file contains "AuxiliaryCommands"
    And the generated "pkg/cmd/root/cmd.go" file contains "RequireChecksum: props.BoolPtr(true)"
    When I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the project manifest contains "endpoint: https://t.example.internal"
    And the generated "pkg/cmd/root/cmd.go" file contains "RequireChecksum: props.BoolPtr(true)"

  Scenario: An unknown config layer is refused at the flag
    Given I generate a gtb project with the flags "--config-layers flags,bogus"
    Then the project exit code is not zero
    And the project output contains "config layer"

  Scenario: The keychain follows the manifest, and a deleted file comes back
    Given a freshly generated gtb project
    Then the generated "cmd/feattool/keychain.go" file exists
    When I delete the generated "cmd/feattool/keychain.go" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "cmd/feattool/keychain.go" file exists
    When I run gtb in the project with "disable keychain"
    Then the project exit code is 0
    And the generated "cmd/feattool/keychain.go" file does not exist
    When I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "cmd/feattool/keychain.go" file does not exist
