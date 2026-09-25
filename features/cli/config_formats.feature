@cli
Feature: A tool reads config files in the formats it links
  A config file's format comes from its extension. YAML needs nothing; any
  other format is read only when the binary links it, and a file in a format
  the binary does not link is refused before anything is read, naming what
  the tool accepts. The e2e binary links TOML and nothing else.

  A tool whose own format changed refuses to start on a file left in the old
  one, and config convert rewrites it.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0204-the-config-stack-a-project-declares-and-orders
  D2 and D14.

  Background:
    Given the gtb binary is built
    And a temporary directory with a config file:
      """
      log:
        level: info
      """

  Scenario: A --config file in a linked format is read through its codec
    Given a config file named "app.toml" with:
      """
      [probe]
      value = "from-toml"
      """
    When I run gtb with "config-probe --config {config_dir}/app.toml"
    Then the exit code is 0
    And stdout contains "config-probe: value=from-toml"

  Scenario: A --config file in a format the tool does not link is refused
    Given a config file named "app.json" with:
      """
      {"probe": {"value": "from-json"}}
      """
    When I run gtb with "config-probe --config {config_dir}/app.json"
    Then the exit code is not 0
    And stderr contains "config file format not linked"
    And stderr contains ".toml"

  Scenario: config convert rewrites a file in another format and keeps the original
    Given a config file named "old.yaml" with:
      """
      probe:
        value: converted
      """
    When I run gtb with "config convert --from {config_dir}/old.yaml --to {config_dir}/new.toml"
    Then the exit code is 0
    And stdout contains "is left in place"
    When I run gtb with "config-probe --config {config_dir}/new.toml"
    Then the exit code is 0
    And stdout contains "config-probe: value=converted"

  Scenario: A config file left in a previous format stops the tool and names the conversion
    Given a config file named ".gtb/config.toml" with:
      """
      [log]
      level = "debug"
      """
    When I run gtb bare with "config get log.level --ci"
    Then the exit code is not 0
    And stderr contains ".gtb/config.toml"
    And stderr contains "config convert --from"
