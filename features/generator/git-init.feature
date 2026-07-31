@generator @integration @smoke
Feature: Generator post-generation git step flags
  gtb generate project makes the destination a real repository with a single
  initial commit by default (opt-out), and can optionally push to the derived
  remote. The flag surface is validated before any files are written.

  Covers the invocation-time flags of
  https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0081-generator-git-initialisation.

  Background:
    Given the gtb binary is built
    And a temporary init directory

  Scenario: Help advertises the git-step flags
    When I run gtb with "generate project --help"
    Then the exit code is 0
    And stdout contains "--no-git"
    And stdout contains "--push"
    And stdout contains "--git-branch"

  Scenario: --no-git together with --push is rejected
    When I run gtb with "generate project --name mytool --repo github.com/myorg/mytool --path {init_dir} --no-git --push --overwrite allow"
    Then the exit code is not 0
    And stderr contains "no-git"
