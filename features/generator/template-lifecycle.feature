@generator @integration
Feature: Custom template-overlay source lifecycle
  The `gtb template` command group manages custom template-overlay sources
  recorded in .gtb/manifest.yaml. Adding a local-folder source records it,
  pins it, and regenerates so its files render over the embedded skeleton;
  `template list` reports it; `template remove` drops it and restores any
  scaffold it replaced. The overlay survives a plain `gtb regenerate project`
  because the pin lives in the manifest.

  The existing custom-templates feature only covers the security rejections and
  the group's --help listing. These scenarios cover the hermetic happy-path
  lifecycle with a local-folder source (no network). The git pin-advancing
  `template update` path needs a real clone and is exercised by the @vcs
  integration suite (internal/generator/templatesource_integration_test.go).

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0080-generator-custom-partial-templates.

  Scenario: Add a local overlay, regenerate keeps it, list reports it, remove drops it
    Given a freshly generated gtb project
    And a local template overlay directory "_overlay" providing a "OVERLAY.md" file
    When I run gtb in the project with "template add ./_overlay --name house"
    Then the project exit code is 0
    And the generated "OVERLAY.md" file contains "overlay for feattool"
    And the project manifest contains "name: house"
    And the project manifest contains "type: local"
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "OVERLAY.md" file contains "overlay for feattool"
    When I run gtb in the project with "template list"
    Then the project exit code is 0
    And the project output contains "house"
    When I run gtb in the project with "template remove house"
    Then the project exit code is 0
    And the project manifest does not contain "name: house"

  Scenario: Removing an unknown source fails clearly
    Given a freshly generated gtb project
    When I run gtb in the project with "template remove nope"
    Then the project exit code is not zero
