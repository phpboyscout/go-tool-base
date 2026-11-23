@generator @integration
Feature: Config directory
  A generated tool keeps its config, update state and trust data in ~/.<tool>.
  A project that records properties.config.dir: xdg keeps them in
  $XDG_CONFIG_HOME/<tool> instead, through props.Tool.Config.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0221-a-tool-can-keep-its-config-under-xdg-config-home (D3).

  Scenario: a fresh project says nothing about its directory, which is home
    Given a freshly generated gtb project
    Then the project manifest does not contain "dir: xdg"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "ConfigDir"

  Scenario: a project generated for the XDG directory renders the field
    Given I generate a gtb project with the flags "--config-dir xdg"
    Then the project manifest contains "dir: xdg"
    And the generated "pkg/cmd/root/cmd.go" file contains "Dir: props.ConfigDirXDG"

  Scenario: set properties.config.dir xdg renders the field and survives regenerate
    Given a freshly generated gtb project
    When I run gtb in the project with "set properties.config.dir xdg"
    Then the project exit code is 0
    And the project manifest contains "dir: xdg"
    And the generated "pkg/cmd/root/cmd.go" file contains "Dir: props.ConfigDirXDG"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file contains "Dir: props.ConfigDirXDG"
    When I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "dir: xdg"

  Scenario: set properties.config.dir home returns to the default rendering
    Given I generate a gtb project with the flags "--config-dir xdg"
    When I run gtb in the project with "set properties.config.dir home"
    Then the project exit code is 0
    And the generated "pkg/cmd/root/cmd.go" file does not contain "ConfigDir"

  Scenario: an unknown directory is refused and nothing changes
    Given a freshly generated gtb project
    When I run gtb in the project with "set properties.config.dir native"
    Then the project exit code is not zero
    And the project manifest does not contain "dir: native"
    And the generated "pkg/cmd/root/cmd.go" file does not contain "ConfigDir"
