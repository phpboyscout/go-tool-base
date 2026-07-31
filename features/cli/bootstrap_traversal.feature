@cli @smoke
Feature: Bootstrap survives a child PersistentPreRunE
  The framework bootstrap (config load, telemetry, feature flags, update
  check) lives in the ROOT command's PersistentPreRunE. A downstream
  subcommand may define its OWN PersistentPreRunE; before
  cobra.EnableTraverseRunHooks was enabled, the child hook shadowed the
  root's bootstrap and props.Config was never populated.

  The contrived "config-probe" fixture (cmd/e2e) defines its own
  PersistentPreRunE (printing a marker) and reads "probe.value" from
  props.Config in RunE. If the root bootstrap ran via root→leaf traversal,
  the value prints; if the child hook had shadowed it, the value would be
  blank. See https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0073-bootstrap-prerun-traversal.

  Background:
    Given the gtb binary is built

  Scenario: Root bootstrap loads config despite a child PersistentPreRunE
    Given a temporary directory with a config file:
      """
      log:
        level: info
      probe:
        value: hello-bootstrap
      """
    When I run gtb with "config-probe"
    Then the exit code is 0
    # The child's own PersistentPreRunE fired — proving both hooks ran.
    And stdout contains "config-probe: child hook ran"
    # The root bootstrap populated props.Config from the --config file;
    # had the child hook shadowed it, the value would be blank.
    And stdout contains "config-probe: value=hello-bootstrap"
