@generator @integration @external-commands
Feature: Attach external command trees to a generated project's root
  `gtb attach command` wires a whole Cobra command tree from an external Go
  module onto a generated project's root as a first-class, regeneration-safe
  manifest entity — replacing the hand-edited cmd/<tool>/main.go + .gtb/ignore
  workaround. Because the attachment lives in .gtb/manifest.yaml and is
  re-rendered into the root on every regenerate, it survives `gtb regenerate`
  and `gtb enable signing` (the exact clobber that motivated the feature).
  `gtb attach adapter` scaffolds the author-owned escape hatch, and
  `gtb detach command` removes an attachment. Covers
  docs/development/specs/2026-07-29-external-command-attachment.md.

  Scenario: Declaratively attaching an external constructor wires it into the root
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdSign(p.GetLogger())"
    And the generated "pkg/cmd/root/cmd.go" file contains "setup.Wrap"
    And the project manifest contains "external_commands"
    And the project manifest contains "gitlab.com/phpboyscout/go/signing-cli"

  Scenario: A second attach for the same module appends another constructor
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is 0
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdKeys --arg logger --wrap"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdSign(p.GetLogger())"
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdKeys(p.GetLogger())"

  Scenario: An attachment survives a full regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdSign(p.GetLogger())"

  Scenario: An attachment survives enable signing re-rendering the root
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is 0
    When I run gtb in the project with "enable signing --email release@acme.test"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdSign(p.GetLogger())"
    And the generated "pkg/cmd/root/cmd.go" file contains "Signing:"

  Scenario: Detaching removes the wiring and the manifest entry
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "signingcli.NewCmdSign(p.GetLogger())"
    When I run gtb in the project with "detach command gitlab.com/phpboyscout/go/signing-cli"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "signingcli"
    And the project manifest does not contain "external_commands"

  Scenario: Attaching the adapter scaffolds the escape hatch and wires it into the root
    Given a freshly generated gtb project
    When I run gtb in the project with "attach adapter"
    Then the project exit code is 0
    And the generated "pkg/cmd/external/attach.go" file exists
    And the generated "pkg/cmd/root/cmd.go" file contains "external.Commands(p)"
    And the project manifest contains "external_commands_adapter: true"

  Scenario: A missing version pin is rejected
    Given a freshly generated gtb project
    When I run gtb in the project with "attach command gitlab.com/phpboyscout/go/signing-cli --constructor NewCmdSign --arg logger --wrap"
    Then the project exit code is not zero
