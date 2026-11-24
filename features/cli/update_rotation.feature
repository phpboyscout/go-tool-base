@cli @rotation
Feature: Self-update across a release key rotation
  When a release key is compromised, the rotation authority signs a manifest
  that revokes it and adds a replacement. Every later release carries that
  manifest, so an installed binary moves to the replacement key on whichever
  update first meets it, and stays there (spec 0101). Each scenario runs its
  own copy of the binary, which trusts only the compromised key and the
  authority, and the channel serves that same binary as each update.

  Background:
    Given the gtb binary is built

  Scenario: Without the rotation manifest, a release signed by the replacement key is refused
    Given a static release channel whose release key was compromised
    And the channel publishes "v1.1.0" signed by the replacement key
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "signature verification failed"

  Scenario: A release carrying the manifest moves the install to the replacement key, and it stays moved
    Given a static release channel whose release key was compromised
    And the channel publishes "v1.1.0" signed by the replacement key with the rotation manifest
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "rotation manifest in force"
    Given the channel publishes "v1.2.0" signed by the replacement key
    When I run gtb with "update"
    Then the exit code is 0
    And stderr does not contain "rotation manifest in force"
    Given the channel publishes "v1.3.0" signed by the compromised key
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "signature verification failed"

  Scenario: An install that skips the release introducing the manifest picks it up from the next one
    Given a static release channel whose release key was compromised
    And the channel publishes "v1.1.0" signed by the replacement key with the rotation manifest
    And the channel publishes "v1.2.0" signed by the replacement key with the rotation manifest
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "rotation manifest in force"
    And stderr contains "v1.2.0"

  Scenario: A home directory that is a git checkout is warned that the rotation is not kept
    Given a static release channel whose release key was compromised
    And the home directory is a git checkout
    And the channel publishes "v1.1.0" signed by the replacement key with the rotation manifest
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "is inside a git checkout"
    Given the channel publishes "v1.2.0" signed by the replacement key
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "trusted again"

  # The forge channel's update side: the real provider reads the release a
  # Gitea or Forgejo server describes, with the manifest as its attachments.
  Scenario Outline: A <forge> forge release carrying the manifest moves the install to the replacement key
    Given a <forge> release channel whose release key was compromised
    And the channel publishes "v1.1.0" signed by the replacement key
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "signature verification failed"
    Given the channel publishes "v1.2.0" signed by the replacement key with the rotation manifest
    When I run gtb with "update"
    Then the exit code is 0
    And stderr contains "rotation manifest in force"
    Given the channel publishes "v1.3.0" signed by the compromised key
    When I run gtb with "update"
    Then the exit code is not 0
    And stderr contains "signature verification failed"

    Examples:
      | forge    |
      | gitea    |
      | codeberg |
