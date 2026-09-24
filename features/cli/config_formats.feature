@cli
Feature: A tool reads config files in the formats it links
  A config file's format comes from its extension. YAML needs nothing; any
  other format is read only when the binary links it, and a file in a format
  the binary does not link is refused before anything is read, naming what
  the tool accepts. The e2e binary links TOML and nothing else.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0204-the-config-stack-a-project-declares-and-orders
  D2.

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
