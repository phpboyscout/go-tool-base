@generator @integration
Feature: The manifest owns every author setting
  An author's settings arrive through flags or the wizard, are recorded in
  .gtb/manifest.yaml, and are read back by regenerate unchanged. A setting that
  only the manifest knew, or that regenerate recomputed from the author's
  machine, is one the author cannot rely on.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0197-author-settings-as-one-surface
  D3, D4, D6, D7, D8, D9, D10 and D13, and
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0199-features-as-a-value-and-a-root-that-owns-its-registries
  D4 (the generated root goes through props.New).

  Scenario: The generated root constructs its Props through props.New and reports a failure as an error
    Given a freshly generated gtb project
    Then the generated "pkg/cmd/root/cmd.go" file contains "props.New("
    And the generated "pkg/cmd/root/cmd.go" file contains "return nil, nil, err"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "&props.Props{"
    And the generated "cmd/feattool/main.go" file contains "rootCmd, p, err := root.NewCmdRoot("
    And the generated "cmd/feattool/main.go" file contains "errorhandling.ExitCodeUsage"

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

  Scenario: A setting is changed after generation with one command
    Given I generate a gtb project with the flags "--features update,init,telemetry"
    Then the project exit code is 0
    When I run gtb in the project with "set telemetry.endpoint https://t.example.internal"
    Then the project exit code is 0
    And the project manifest contains "endpoint: https://t.example.internal"
    And the generated "pkg/cmd/root/cmd.go" file contains "t.example.internal"
    When I run gtb in the project with "get telemetry.endpoint"
    Then the project exit code is 0
    And the project output contains "https://t.example.internal"
    When I run gtb in the project with "unset telemetry.endpoint"
    Then the project exit code is 0
    And the project manifest does not contain "t.example.internal"

  Scenario: Setting the chat default rewrites the defaults bundle
    Given I generate a gtb project with features "init,update,ai", chat providers "claude,openai" and chat default "claude"
    Then the project exit code is 0
    When I run gtb in the project with "set chat.default.provider openai"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat/assets/config.yaml" file contains "openai"
    And the generated "cmd/feattool/chat/assets/config.yaml" file does not contain "claude"
    When I run gtb in the project with "set chat.default.provider gemini"
    Then the project exit code is not zero
    And the project output contains "not one the tool links"

  Scenario: A path the table does not name is refused with the list
    Given a freshly generated gtb project
    When I run gtb in the project with "set bogus.path x"
    Then the project exit code is not zero
    And the project output contains "chat.default.provider"
    When I run gtb in the project with "set features ai"
    Then the project exit code is not zero
    And the project output contains "enable"

  Scenario: Verification can be skipped, and the run says so
    Given I generate a gtb project with the flags "--no-verify"
    Then the project exit code is 0
    And the project output contains "not verified"

  Scenario: The wizard needs a terminal and says what to use instead
    Given a freshly generated gtb project
    When I run gtb in the project with "wizard --dry-run"
    Then the project exit code is not zero
    And the project output contains "gtb set"
