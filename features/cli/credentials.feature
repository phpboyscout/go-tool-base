@cli @integration
Feature: Credential storage hardening
  GTB resolves credentials in this precedence order:
  <prefix>.env > <prefix>.keychain > <prefix>.value/key.
  The `doctor` command reports literal credentials that remain in the
  config, and the `config migrate-credentials` command moves them to
  environment variable references or the OS keychain.

  Background:
    Given the gtb binary is built

  Scenario: Doctor warns when literal credentials are present
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          key: sk-ant-scenario-secret
      github:
        auth:
          value: ghp_scenario_secret
      """
    # --fail-on is stated rather than inherited: the default differs under CI,
    # and a scenario whose expected exit code depends on where it runs is a
    # scenario that will disagree with the pipeline eventually.
    When I run gtb with "doctor --fail-on=none"
    Then the exit code is 0
    And stdout contains "[!!] Credential storage"
    And stdout contains "2 literal credential"
    And stdout contains "anthropic.api.key"
    And stdout contains "github.auth.value"
    And stdout does not contain "sk-ant-scenario-secret"
    And stdout does not contain "ghp_scenario_secret"

  Scenario: Literal credentials fail a gated run
    # R3: deprecated storage must be able to stop a pipeline, or the
    # compatibility window never closes. This is the default under CI.
    Given a temporary directory with a config file:
      """
      anthropic:
        api:
          key: sk-ant-scenario-secret
      """
    When I run gtb with "doctor --fail-on=warn"
    Then the exit code is 1
    And stdout contains "[!!] Credential storage"

  Scenario: An advisory warning does not fail a gated run
    # Only a warning a check marks as a policy violation gates. "No AI provider
    # configured" is a fine state for a tool that does not use AI, and failing
    # its pipeline over that would turn a diagnostic into a tripwire.
    Given a temporary directory with a config file:
      """
      anthropic:
        api:
          env: SOME_ANTHROPIC_VAR
      """
    When I run gtb with "doctor --fail-on=warn"
    Then the exit code is 0

  Scenario: Doctor passes when credentials use env-var references
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          env: ANTHROPIC_API_KEY
      """
    When I run gtb with "doctor --fail-on=none"
    Then the exit code is 0
    And stdout contains "[OK] Credential storage"

  Scenario: Migrate dry-run prints plan without writing
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          key: sk-ant-scenario-secret
      """
    When I run gtb with "config migrate-credentials --dry-run --yes"
    Then the exit code is 0
    And stdout contains "Migration plan (dry run"
    And stdout contains "anthropic.api.key"
    And stdout contains "anthropic.api.env"
    And stdout contains "ANTHROPIC_API_KEY"
    And stdout contains "target: env"
    And the config file contains "key: sk-ant-scenario-secret"
    And the config file does not contain "env: ANTHROPIC_API_KEY"

  Scenario: Migrate applies env-var default and clears literals
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          key: sk-ant-scenario-secret
      github:
        auth:
          value: ghp_scenario_secret
      """
    When I run gtb with "config migrate-credentials --yes"
    Then the exit code is 0
    And stdout contains "Migration complete"
    And the config file contains "env: ANTHROPIC_API_KEY"
    And the config file contains "env: GITHUB_TOKEN"
    And the config file does not contain "sk-ant-scenario-secret"
    And the config file does not contain "ghp_scenario_secret"

  Scenario: Bitbucket dual-credential migrates both halves together
    Given a temporary directory with a config file:
      """
      log:
        level: info
      bitbucket:
        username: alice-scenario-user
        app_password: ATBB-scenario-secret
      """
    When I run gtb with "config migrate-credentials --yes"
    Then the exit code is 0
    And stdout contains "bitbucket.username + bitbucket.app_password"
    And the config file contains "env: BITBUCKET_USERNAME"
    And the config file contains "env: BITBUCKET_APP_PASSWORD"
    And the config file does not contain "alice-scenario-user"
    And the config file does not contain "ATBB-scenario-secret"

  Scenario: Migration is idempotent when no literals remain
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          env: ANTHROPIC_API_KEY
      """
    When I run gtb with "config migrate-credentials --yes"
    Then the exit code is 0
    And stdout contains "No literal credentials found"

  # A credential stored as an env-var reference must not be mistaken for one set
  # to the empty string. Every forge ships its keys as a subtree
  # (bitbucket.app_password.env), and startup validation asked "is this key set
  # and empty?" — which a mapping answers yes to. The result was a warning on
  # every command, about a credential that was configured correctly.
  #
  # Asserted through the CLI because that is where it was visible: the unit test
  # pins the predicate, this pins that nothing reaches the operator's terminal.
  @smoke
  Scenario: A subtree-shaped credential produces no empty-key warning
    Given a temporary directory with a config file:
      """
      log:
        level: info
      """
    When I run gtb with "config get log.level"
    Then the exit code is 0
    And stderr does not contain "is set but empty"

  # ----- credential RESOLUTION (spec 0183) ----------------------------

  # Storage and resolution are different questions, and only the first had a
  # command behind it. The scenarios above cover where a credential is written;
  # these cover whether it can be read back and from which rung — the precedence
  # GTB has owned since go/forge v0.8.0 moved ordering out of the module and
  # into the consumer's config stack.
  #
  # Every scenario scrubs the well-known fallback variable first. Without that
  # they pass for the wrong reason on any developer machine whose shell exports
  # a real GITHUB_TOKEN, while failing in CI where it is absent — which is the
  # worst combination a test can have.

  @smoke
  Scenario: An environment-variable reference resolves, and the rung is named
    Given a temporary directory with a config file:
      """
      log:
        level: info
      github:
        auth:
          env: MY_FORGE_TOKEN
      """
    And I set environment variable "GITHUB_TOKEN" to ""
    And I set environment variable "MY_FORGE_TOKEN" to "tok-scenario-secret"
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "GitHub credential: resolves from auth.env"
    And stdout does not contain "tok-scenario-secret"

  Scenario: A literal credential resolves, and is still reported as a literal
    Given a temporary directory with a config file:
      """
      log:
        level: info
      github:
        auth:
          value: tok-scenario-secret
      """
    And I set environment variable "GITHUB_TOKEN" to ""
    # The literal is the point of this scenario, so the gate is stated off:
    # what is being asserted is the REPORT, not the exit code.
    When I run gtb with "doctor --fail-on=none"
    Then the exit code is 0
    And stdout contains "GitHub credential: resolves from auth.value"
    And stdout contains "[!!] Credential storage"
    And stdout does not contain "tok-scenario-secret"

  # GITHUB_TOKEN still works with nothing in the user's config — but it arrives
  # through auth.env, not through the fallback rung, because the forge's own
  # embedded bundle ships `github.auth.env: GITHUB_TOKEN` as a default. Rung 1
  # therefore already reads the variable rung 4 would have read, and reports
  # itself as the origin.
  #
  # This is the documented precedence behaving correctly, not a bug: same
  # variable, same value, same outcome. It does mean the fallback rung is
  # effectively shadowed for every forge that ships that default, which is worth
  # knowing before reading a report and concluding the fallback is broken.
  Scenario: The well-known variable resolves with nothing in the user's config
    Given a temporary directory with a config file:
      """
      log:
        level: info
      """
    And I set environment variable "GITHUB_TOKEN" to "tok-scenario-secret"
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "GitHub credential: resolves from auth.env"
    And stdout does not contain "tok-scenario-secret"

  # The case the check exists for. Before it, a broken reference was
  # indistinguishable from having no credential at all until something tried to
  # authenticate and came back with a bare 401.
  Scenario: A broken keychain reference is diagnosed rather than reported as absent
    Given a temporary directory with a config file:
      """
      log:
        level: info
      github:
        auth:
          keychain: no-slash-here
      """
    And I set environment variable "GITHUB_TOKEN" to ""
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "GitHub credential: credential configured but does not resolve"
    And stdout contains "malformed keychain reference"

  Scenario: A forge with no credential is skipped, not failed
    Given a temporary directory with a config file:
      """
      log:
        level: info
      """
    And I set environment variable "GITHUB_TOKEN" to ""
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "GitHub credential: no credential configured"

  Scenario: Keychain target dry-run prints keychain destination in the plan
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          key: sk-ant-scenario-secret
      """
    When I run gtb with "config migrate-credentials --target=keychain --dry-run --yes"
    Then the exit code is 0
    And stdout contains "Migration plan (dry run"
    And stdout contains "anthropic.api.keychain"
    And stdout contains "target: keychain"
    And the config file contains "key: sk-ant-scenario-secret"
    And the config file does not contain "keychain:"
