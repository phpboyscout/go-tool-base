@generator @integration
Feature: add-flag preserves command metadata
  Adding a flag to an existing command regenerates that command's
  cmd.go from its full manifest record. The regeneration must not drop
  the command's aliases, required/shorthand flags, or pre-run hooks,
  and must write the refreshed cmd.go hash back to the manifest.

  Regression guard for the `add-flag-regen-drops-command-metadata`
  audit finding.

  Scenario: Aliases, required shorthand flag, and pre-run hook survive add-flag
    Given a gtb project with a "deploy" command that has aliases, a required shorthand flag, and a pre-run hook
    When I run gtb in the project with "generate add-flag --command deploy --name verbose --type bool --description Verbose"
    Then the project exit code is 0
    And the generated "pkg/cmd/deploy/cmd.go" file contains "dep"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "ship"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "MarkFlagRequired"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "PersistentPreRunE"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "verbose"
    And the project manifest contains "persistent_pre_run: true"
    And the project manifest contains "shorthand: t"
    And the project manifest contains "required: true"
    And the project manifest contains "name: verbose"
    And the project manifest does not contain "stalehash"
