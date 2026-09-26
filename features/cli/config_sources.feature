@cli
Feature: A tool reads configuration from the sources it declares
  A tool may read configuration from a source beyond its files: a Consul
  prefix, a Vault path, a bucket. Each is a named slot the tool declares.
  Where the slot connects is the user's to configure, with
  `init config <name>`, and a required slot nobody configured stops the tool
  and names that command. The e2e binary declares a required slot named team
  when GTB_E2E_CONFIG_SOURCE is set, of a kind that reads a local file.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0204-the-config-stack-a-project-declares-and-orders
  D3, D4, D6 and R5.

  Background:
    Given the gtb binary is built
    And I set environment variable "GTB_E2E_CONFIG_SOURCE" to "{config_dir}/team.yaml"
    And a config file named "team.yaml" with:
      """
      probe:
        value: from-the-source
      """

  Scenario: A required source nobody configured stops the tool and names the command
    When I run gtb with "config-probe"
    Then the exit code is not 0
    And stderr contains "gtb init config team"

  Scenario: init config writes the source's settings and the tool then reads from it
    When I run gtb with "init config team --dir {config_dir}"
    Then the exit code is 0
    And the config file contains "team.yaml"
    When I run gtb with "config-probe"
    Then the exit code is 0
    And stdout contains "config-probe: value=from-the-source"

  Scenario: doctor still runs while the source is unconfigured
    When I run gtb with "doctor"
    Then stdout contains "Configuration"
    And stderr does not contain "gtb.setup.config_source_unconfigured"
