@generator @integration
Feature: A generated tool declares its config source slots
  A source slot is a named, typed place in the config stack: the manifest
  says which kind it is and where it sits, and each operator configures it
  with `<tool> init config <name>`. A shipped kind is linked by a blank import
  in cmd/<name>/config.go; an override-only kind is built by the author's own
  code, so nothing is linked for it. Declaring a slot without a layer list
  places it above the defaults and below the user's own files.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0204-the-config-stack-a-project-declares-and-orders
  D3, D12, D15, D21 and R9.

  Scenario: Declared slots are recorded, placed, rendered and linked
    Given I generate a gtb project with the flags "--config-source team=consul --config-source legacy=etcd --config-source-optional team"
    Then the project exit code is 0
    And the project manifest contains "sources:"
    And the project manifest contains "kind: consul"
    And the project manifest contains "required: false"
    And the project manifest contains "env_prefix: FEATTOOL"
    And the generated "cmd/feattool/config.go" file contains "gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/consul"
    And the generated "cmd/feattool/config.go" file does not contain "sources/etcd"
    And the generated "pkg/cmd/root/cmd.go" file contains 'props.ConfigLayer("team")'
    And the generated "pkg/cmd/root/cmd.go" file contains "props.BoolPtr(false)"

  Scenario: A manifest rebuilt from scratch recovers the slots
    Given I generate a gtb project with the flags "--config-source team=consul --config-source-optional team"
    When I delete the generated ".gtb/manifest.yaml" file
    And I run gtb in the project with "regenerate manifest"
    Then the project exit code is 0
    And the project manifest contains "kind: consul"
    And the project manifest contains "required: false"
    And the project manifest contains "- team"

  Scenario: A keychain slot links the keychain
    Given I generate a gtb project with the flags "--config-source tokens=keychain"
    Then the project exit code is 0
    And the generated "cmd/feattool/keychain.go" file exists

  Scenario: An unknown kind is refused
    Given I generate a gtb project with the flags "--config-source team=zookeeper"
    Then the project exit code is not zero
    And the project output contains "unknown config source kind"

  Scenario: A slot list must name a declared slot
    Given I generate a gtb project with the flags "--config-source team=consul --config-source-writable ghost"
    Then the project exit code is not zero
    And the project output contains "--config-source ghost=<kind>"

  Scenario: Slots are not set one key at a time
    Given a freshly generated gtb project
    When I run gtb in the project with "set config.sources team"
    Then the project exit code is not zero
    And the project output contains "--config-source"
