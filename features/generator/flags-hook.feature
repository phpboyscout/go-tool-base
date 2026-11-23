@generator @integration
Feature: A command's hand-written code can add flags
  A command generated with --flags-hook calls a hand-written Flags function
  while it is built, so a flag can depend on what the binary links. Its run
  reads the same flag set, and can tell a flag the user set to its default
  from one left alone (spec 0224).

  Background:
    Given a freshly generated gtb project

  Scenario: A hook-added flag reaches the run
    When I run gtb in the project with "generate command --name sign --short Sign --flags-hook"
    Then the project exit code is 0
    And the project manifest contains "flags_hook: true"
    And the generated "pkg/cmd/sign/main.go" file contains "func FlagsSign(props *props.Props, flags *pflag.FlagSet)"
    When I write a flags hook adding "region" with default "eu-west-1" to the generated "sign" command
    And I build the generated project
    And I run the built tool with "sign"
    Then the project exit code is 0
    And the project output contains "region=eu-west-1 changed=false"
    When I run the built tool with "sign --region us-east-1"
    Then the project output contains "region=us-east-1 changed=true"
    When I run the built tool with "sign --region eu-west-1"
    Then the project output contains "region=eu-west-1 changed=true"

  Scenario: A declared flag that takes the hook's field is refused
    When I run gtb in the project with "generate command --name sign --short Sign --flags-hook --flag flag-set:string:Set"
    Then the project exit code is 1
    And the project output contains "flag-set"
    And the generated "pkg/cmd/sign/cmd.go" file does not exist
