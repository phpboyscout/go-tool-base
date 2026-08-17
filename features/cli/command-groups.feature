@cli @smoke
Feature: A command group reports a verb it does not have
  A gtb command that only groups subcommands answers a bare invocation with its
  usage and succeeds — that is a request for help, not a failure. A verb it does
  not have is a mistake, and it says so and exits 2.

  Cobra cannot arrange this by itself. A command with no RunE returns
  flag.ErrHelp before ValidateArgs is reached, so cobra.NoArgs on a group is
  silently inert; and cobra's own unknown-command report is produced for the ROOT
  only. Every group below the root therefore answered a typo with help and exit 0,
  which told the user nothing and gave a script no way to tell a typo from a
  command that ran.

  Implements spec 0191.

  Background:
    Given the gtb binary is built

  Scenario: A bare command group prints its verbs and succeeds
    When I run gtb with "generate"
    Then the exit code is 0
    # cmd.Usage() writes to stderr, where cobra sends usage; --help goes to
    # stdout. That split is cobra's and predates this change.
    And stderr contains "Available Commands"
    And stderr contains "project"

  Scenario: A command group rejects a verb it does not have
    When I run gtb with "generate zzbogus"
    Then the exit code is 2
    And stderr contains "unknown command"
    And stderr contains "zzbogus"
    And stderr contains "gtb generate"

  Scenario: A group that does work of its own is unaffected
    # `doctor` has children AND runs the checks itself, so it owns its RunE.
    When I run gtb with "doctor --help"
    Then the exit code is 0
    And stdout contains "Available Commands"
