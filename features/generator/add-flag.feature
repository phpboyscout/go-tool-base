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

  Scenario: add-flag can set a single-letter shorthand
    Given a gtb project with a "deploy" command that has aliases, a required shorthand flag, and a pre-run hook
    When I run gtb in the project with "generate add-flag --command deploy --name output --type string --description Output --shorthand o"
    Then the project exit code is 0
    And the project manifest contains "name: output"
    And the project manifest contains "shorthand: o"
    And the generated "pkg/cmd/deploy/cmd.go" file contains "StringVarP"

  Scenario: a kept cmd.go fails add-flag, and the same command with overwrite applies it
    # Issue #238: the run ended in success and the tool had no such flag.
    Given a freshly generated gtb project
    When I run gtb in the project with "generate command --name post --short posts"
    And I hand-edit the generated "pkg/cmd/post/cmd.go" file
    And I run gtb in the project with "generate add-flag --command post --name loud --type bool"
    Then the project exit code is not zero
    And the project manifest contains "name: loud"
    And the project output does not contain "added flag to command"
    And the generated "pkg/cmd/post/cmd.go" file does not contain "loud"
    When I run gtb in the project with "generate add-flag --command post --name loud --type bool --overwrite allow"
    Then the project exit code is 0
    And the generated "pkg/cmd/post/cmd.go" file contains '"loud"'
