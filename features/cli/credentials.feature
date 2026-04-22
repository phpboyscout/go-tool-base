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
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "[!!] Credential storage"
    And stdout contains "2 literal credential"
    And stdout contains "anthropic.api.key"
    And stdout contains "github.auth.value"
    And stdout does not contain "sk-ant-scenario-secret"
    And stdout does not contain "ghp_scenario_secret"

  Scenario: Doctor passes when credentials use env-var references
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          env: ANTHROPIC_API_KEY
      """
    When I run gtb with "doctor"
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
