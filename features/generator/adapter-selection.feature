@generator @integration
Feature: A generated tool links only the adapters it selects
  Every chat provider and forge adapter is a module a tool blank-imports from
  its own main package. The generator derives those imports from the manifest:
  chat providers from chat.providers under the ai feature, forge adapters from
  the enabled forge features. A project generated before the chat block
  existed keeps every provider it had.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0194-the-gtb-cli-as-a-nested-module-and-adapters-chosen-per-tool
  D4 to D8.

  Scenario: A project without the ai feature links no chat provider and no forge
    Given a freshly generated gtb project
    Then the generated "cmd/feattool/chat.go" file exists
    And the generated "cmd/feattool/chat.go" file does not contain "gitlab.com/phpboyscout/go/chat-"
    And the generated "cmd/feattool/forge.go" file exists
    And the generated "cmd/feattool/forge.go" file does not contain "gitlab.com/phpboyscout/go/forge-"
    And the project manifest does not contain "providers:"

  Scenario: Selecting ai and one provider links exactly that provider's module
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude-local"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains "_ \"gitlab.com/phpboyscout/go/chat-anthropic\""
    And the generated "cmd/feattool/chat.go" file does not contain "chat-openai"
    And the generated "cmd/feattool/chat.go" file does not contain "chat-gemini"
    And the project manifest contains "- claude-local"
    And the project manifest does not contain "- gemini"

  Scenario: An enabled forge feature links its adapter, and codeberg shares gitea's
    Given I generate a gtb project with features "init,update,github,codeberg" and chat providers ""
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains "_ \"gitlab.com/phpboyscout/go/forge-github\""
    And the generated "cmd/feattool/forge.go" file contains "_ \"gitlab.com/phpboyscout/go/forge-gitea\""
    And the generated "cmd/feattool/forge.go" file does not contain "forge-gitlab"

  Scenario: The ai feature with no provider is refused at generation
    Given I generate a gtb project with features "init,update,ai" and chat providers ""
    Then the project exit code is not zero
    And the project output contains "at least one chat provider"

  Scenario: A provider no module registers is refused at generation
    Given I generate a gtb project with features "init,update,ai" and chat providers "chatgpt"
    Then the project exit code is not zero
    And the project output contains "not one a known module registers"

  Scenario: Enabling ai on a project with no chat block records the default providers
    Given a freshly generated gtb project
    When I run gtb in the project with "enable ai"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the project manifest contains "- claude-local"
    And the project manifest contains "- openai-compatible"
    And the generated "cmd/feattool/chat.go" file contains "chat-anthropic"
    And the generated "cmd/feattool/chat.go" file contains "chat-openai"
    And the generated "cmd/feattool/chat.go" file contains "chat-gemini"

  Scenario: An explicit empty provider list stays empty across regenerates
    Given I generate a gtb project with features "update,init,docs,doctor,ai" and chat providers "claude"
    Then the project exit code is 0
    When I set the project manifest chat providers to none
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the project manifest contains "providers: []"
    And the generated "cmd/feattool/chat.go" file does not contain "go/chat-"
    When I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the project manifest contains "providers: []"
    And the project manifest does not contain "- gemini"
    And the generated "cmd/feattool/chat.go" file does not contain "go/chat-"

  Scenario: Disabling a forge feature removes its adapter on regenerate
    Given I generate a gtb project with features "init,update,github" and chat providers ""
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains "forge-github"
    When I run gtb in the project with "disable github"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file does not contain "forge-github"

  Scenario: The generated go.mod pins gtb by its nested-module path
    Given a freshly generated gtb project
    Then the generated "go.mod" file contains "gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb"
