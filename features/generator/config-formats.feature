@generator @integration
Feature: A generated tool links the config formats it declares
  A tool reads YAML config files with nothing linked. Every other format of
  the go/config family is declared in the manifest and linked by a blank
  import in cmd/<name>/config.go, so a tool that declares none carries none
  of their dependencies. The tool's own config file may be in any linked
  format that can be written.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0204-the-config-stack-a-project-declares-and-orders
  D2 and D8.

  Scenario: Declared formats are linked and the own format is rendered
    Given I generate a gtb project with the flags "--config-formats toml,dotenv --config-format toml"
    Then the project exit code is 0
    And the project manifest contains "formats:"
    And the project manifest contains "format: toml"
    And the generated "cmd/feattool/config.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/toml"
    And the generated "cmd/feattool/config.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/dotenv"
    And the generated "pkg/cmd/root/cmd.go" file contains "props.ConfigSpec{Format: \"toml\"}"

  Scenario: A tool that declares no format links none, and gtb set links one
    Given a freshly generated gtb project
    Then the generated "cmd/feattool/config.go" file does not exist
    And the generated "pkg/cmd/root/cmd.go" file does not contain "ConfigSpec"
    When I run gtb in the project with "set config.formats json"
    Then the project exit code is 0
    And the generated "cmd/feattool/config.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/json"

  Scenario: The own format must be linked
    Given I generate a gtb project with the flags "--config-format toml"
    Then the project exit code is not zero
    And the project output contains "--config-formats"

  Scenario: A read-only format cannot be the tool's own
    Given I generate a gtb project with the flags "--config-formats ini --config-format ini"
    Then the project exit code is not zero
    And the project output contains "yaml, toml, json or hcl"
