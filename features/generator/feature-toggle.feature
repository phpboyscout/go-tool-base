@generator @integration
Feature: Enable and disable built-in features post-generation
  `gtb enable <feature>` and `gtb disable <feature>` flip a built-in feature in
  .gtb/manifest.yaml and re-render the generated root command, so the feature
  set can change after creation without hand-editing the DO-NOT-EDIT root — and
  the change survives `gtb regenerate project`.

  Remediates keryx bug-report #8 part 2. Covers
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0086-enable-disable-features.

  Scenario: Enabling an opt-in feature wires it into the root and survives regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "enable ai"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(props.AiCmd)"
    And the project manifest contains "name: ai"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(props.AiCmd)"

  Scenario: Enabling several features in one invocation
    Given a freshly generated gtb project
    When I run gtb in the project with "enable ai config telemetry"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(props.AiCmd)"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(props.ConfigCmd)"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(props.TelemetryCmd)"

  Scenario: Disabling a default-on feature wires Disable into the root
    Given a freshly generated gtb project
    When I run gtb in the project with "disable doctor"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Disable(props.DoctorCmd)"

  Scenario: An unknown feature is rejected
    Given a freshly generated gtb project
    When I run gtb in the project with "enable bogus"
    Then the project exit code is not zero

  # A forge feature declares its constant in pkg/setup/forge, not props. The
  # emitter used to hard-code the props qualifier, so a selected forge was
  # accepted, written to the manifest, and then silently dropped at emission —
  # exit 0 for a tool that did not have the feature. Covers spec 0185.
  Scenario: A forge feature selected at generation reaches the root and survives regenerate
    When I generate a gtb project with features "init,update,docs,gitlab"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(forge.GitlabFeature)"
    And the generated "pkg/cmd/root/cmd.go" file contains "pkg/setup/forge"
    And the project manifest contains "name: gitlab"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(forge.GitlabFeature)"

  Scenario: A forge feature can be toggled post-generation like any other
    Given a freshly generated gtb project
    When I run gtb in the project with "enable gitea"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "props.Enable(forge.GiteaFeature)"

  Scenario: signing remains a scoped subcommand, not a feature
    Given a freshly generated gtb project
    When I run gtb in the project with "enable signing --email release@acme.test"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "Signing:"
