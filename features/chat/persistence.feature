@chat @smoke
Feature: Chat Conversation Persistence
  Tool authors can save, restore, list, and delete conversation snapshots
  using the FileStore, with optional encryption.

  Scenario: Save and load a conversation snapshot
    Given a new FileStore
    And a conversation snapshot for provider "claude" with model "claude-3-5-sonnet"
    When I save the snapshot to the store
    And I load the snapshot by ID
    Then the loaded snapshot matches the original
    And the loaded snapshot has provider "claude"
    And the loaded snapshot has model "claude-3-5-sonnet"

  Scenario: List stored snapshots
    Given a new FileStore
    And a conversation snapshot with ID "snap-1"
    And a conversation snapshot with ID "snap-2"
    And a conversation snapshot with ID "snap-3"
    When I save all snapshots to the store
    And I list snapshots
    Then the list contains 3 summaries

  Scenario: Delete a snapshot
    Given a new FileStore
    And a conversation snapshot with ID "to-delete"
    When I save the snapshot to the store
    And I delete the snapshot by ID
    Then loading the snapshot by ID fails

  Scenario: Encrypted snapshots are not readable as plaintext
    Given a new encrypted FileStore
    And a conversation snapshot with system prompt "top secret instructions"
    When I save the snapshot to the store
    Then the raw file does not contain "top secret instructions"
    And I load the snapshot by ID
    And the loaded snapshot has system prompt "top secret instructions"

  Scenario: Wrong encryption key fails to load
    Given a new encrypted FileStore
    And a conversation snapshot with ID "encrypted-snap"
    When I save the snapshot to the store
    And I create a FileStore with a different encryption key
    Then loading the snapshot by ID fails

  Scenario: Provider mismatch on restore is rejected
    Given a conversation snapshot for provider "claude" with model "claude-3-5-sonnet"
    When I attempt to restore it into an "openai" provider
    Then the restore fails with a provider mismatch error

  Scenario: Tool handlers are excluded from snapshots
    Given a conversation snapshot with tools
    Then the snapshot tools contain names and descriptions
    And the snapshot tools do not contain handlers
