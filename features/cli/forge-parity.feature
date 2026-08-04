@cli @integration
Feature: CLI Forge Credential Commands
  Every forge adapter the framework registers has an `init <forge>`
  command behind it, so a user whose primary forge is not GitHub has an
  interactive path to configure credentials.

  Before this, GitLab had a registered provider, a generator wizard
  option, a config section accepted by validation and migrate entries —
  and no way to configure a credential at all.

  These scenarios cover the operator-visible surface: that each command
  exists, is discoverable, and writes the forge's own config keys. The
  credential-capture wizards themselves are interactive, so the storage
  modes are covered by the unit suites in pkg/setup/forge.

  Background:
    Given the gtb binary is built
    And a temporary init directory

  # ----- discoverability ----------------------------------------------

  @smoke
  Scenario Outline: Each registered forge has an init subcommand
    When I run gtb with "init <forge> --help"
    Then the exit code is 0
    And stdout contains "<label>"

    Examples:
      | forge     | label     |
      | github    | GitHub    |
      | gitlab    | GitLab    |
      | gitea     | Gitea     |
      | codeberg  | Codeberg  |
      | bitbucket | Bitbucket |

  @smoke
  Scenario: The init command lists every forge subcommand
    When I run gtb with "init --help"
    Then the exit code is 0
    And stdout contains "github"
    And stdout contains "gitlab"
    And stdout contains "gitea"
    And stdout contains "codeberg"
    And stdout contains "bitbucket"

  # ----- Gitea has no interactive login -------------------------------

  Scenario: Gitea's help states the token is entered by hand
    When I run gtb with "init gitea --help"
    Then the exit code is 0
    And stdout contains "does not support an interactive browser login"

  # Codeberg runs through the same forge-gitea adapter, so the same upstream
  # fact applies: no Authenticator, therefore no browser login to offer.
  Scenario: Codeberg's help states the token is entered by hand
    When I run gtb with "init codeberg --help"
    Then the exit code is 0
    And stdout contains "does not support an interactive browser login"

  Scenario: GitHub's help does not claim a manual-only token
    When I run gtb with "init github --help"
    Then the exit code is 0
    And stdout does not contain "does not support an interactive browser login"

  # ----- SSH is a capability, not a credential shape (0186) -----------

  # forge-bitbucket implements KeyManager, but GTB could not reach it: the SSH
  # stage was called only from the single-token flow. Whether a forge is offered
  # a key is now a property of its profile.
  @smoke
  Scenario: A dual-credential forge offers SSH keys too
    When I run gtb with "init bitbucket --help"
    Then the exit code is 0
    And stdout contains "SSH key"
    And stdout contains "--skip-key"

  # The summary line comes from each command's Short, which appears in the
  # parent's subcommand listing rather than in the subcommand's own --help.
  Scenario Outline: Every forge that offers SSH says so in the init listing
    When I run gtb with "init --help"
    Then the exit code is 0
    And stdout contains "<summary>"

    Examples:
      | summary                                                                   |
      | Configure GitHub authentication and SSH keys                              |
      | Configure GitLab authentication and SSH keys                              |
      | Configure Gitea authentication and SSH keys                               |
      | Configure Codeberg authentication and SSH keys                            |
      | Configure Bitbucket authentication (username + app password) and SSH keys |

  # ----- skip flags keep init non-interactive -------------------------

  Scenario: Non-interactive init skips every forge wizard
    When I run gtb with "init --skip-login --skip-key --skip-ai --skip-gitlab --skip-gitea --skip-codeberg --skip-bitbucket --dir {init_dir}"
    Then the exit code is 0
    And the file "config.yaml" exists in the init directory

  # The per-forge embedded bundles supply merged config *defaults* rather
  # than seeding the written file, so "Sub(<forge>) is non-nil" is asserted
  # against the registry in pkg/setup/forge (TestEveryForgeShipsAConfigBundle)
  # rather than through the CLI, where it would test the wrong surface.
