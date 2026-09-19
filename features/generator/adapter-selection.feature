@generator @integration
Feature: A generated tool links only the adapters it selects
  Every chat provider and forge adapter is a module a tool blank-imports from
  its own main package. The generator derives those imports from the manifest:
  chat providers from chat.providers under the ai feature, forge adapters from
  the enabled forge features, which the forge backend and --forge-credentials
  imply. A project generated before the chat block existed keeps every
  provider it had.

  Covers https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0194-the-gtb-cli-as-a-nested-module-and-adapters-chosen-per-tool
  D4 to D8 and https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0195-the-forge-backend-implies-the-forge
  D1, D4, D6, D8 and D10.

  Scenario: A default project links its backend's forge and no chat provider
    Given a freshly generated gtb project
    # Nothing the tool does not use leaves a file behind: chat.go exists while
    # a provider is linked or the ai feature is on, forge.go while a forge is.
    Then the generated "cmd/feattool/chat.go" file does not exist
    And the generated "cmd/feattool/forge.go" file contains "gitlab.com/phpboyscout/go/forge-github"
    And the generated "cmd/feattool/forge.go" file does not contain "forge-gitlab"
    And the project manifest contains "backend: github"
    And the project manifest contains "type: github"
    And the project manifest does not contain "providers:"

  Scenario: The forge backend decides the release source and the CI skeleton, not the host
    Given I generate a gtb project with forge backend "gitlab" on host "code.example.com"
    Then the project exit code is 0
    And the project manifest contains "type: gitlab"
    And the project manifest contains "backend: gitlab"
    And the project manifest contains "host: code.example.com"
    And the generated ".gitlab-ci.yml" file exists
    And the generated ".github/workflows" file does not exist
    And the generated "cmd/feattool/forge.go" file contains "forge-gitlab"

  Scenario: A backend without a CI skeleton is offered and writes no CI files
    Given I generate a gtb project with forge backend "gitea" on host "git.example.com"
    Then the project exit code is 0
    And the project manifest contains "type: gitea"
    And the generated "cmd/feattool/forge.go" file contains "forge-gitea"
    And the generated ".gitlab-ci.yml" file does not exist
    And the generated ".github" file does not exist
    And the project output contains "no CI skeleton"

  Scenario: A forge name in --features is refused and names the flag that chooses one
    Given I generate a gtb project with features "update,github"
    Then the project exit code is not zero
    And the project output contains "--forge-backend"

  Scenario: A tool links providers for its own code without the ai feature
    # The provider list is a shortcut for wiring go/chat modules; the ai feature
    # switches GTB's AI-based features and is not needed to link (go-tool-base #94).
    Given I generate a gtb project with features "init,update" and chat providers "claude,gemini"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains "gitlab.com/phpboyscout/go/chat-anthropic"
    And the generated "cmd/feattool/chat.go" file contains "gitlab.com/phpboyscout/go/chat-gemini"
    And the generated "cmd/feattool/chat.go" file contains 'props.DeclareLinks(props.ChatLinkPrefix, "claude", "gemini")'
    And the project manifest contains "- claude"
    And the project manifest does not contain "name: ai"
    And the generated "cmd/feattool/chat/assets/config.yaml" file does not exist

  Scenario: Selecting ai and one provider links exactly that provider's module
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude-local"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains "gitlab.com/phpboyscout/go/chat-anthropic"
    And the generated "cmd/feattool/chat.go" file does not contain "chat-openai"
    And the generated "cmd/feattool/chat.go" file does not contain "chat-gemini"
    And the project manifest contains "- claude-local"
    And the project manifest does not contain "- gemini"

  Scenario: The chosen providers are declared as link features, not just imported
    Given I generate a gtb project with features "init,update,ai", chat providers "claude,codex-local" and chat default "claude"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains 'props.DeclareLinks(props.ChatLinkPrefix, "claude", "codex-local")'
    And the generated "cmd/feattool/forge.go" file contains 'props.DeclareLinks(props.ForgeLinkPrefix, "github")'

  Scenario: A disabled forge leaves the link declaration as well as the import
    Given I generate a gtb project with forge backend "gitlab" and forge credentials "github"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains 'props.DeclareLinks(props.ForgeLinkPrefix, "github", "gitlab")'
    When I run gtb in the project with "disable github"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains 'props.DeclareLinks(props.ForgeLinkPrefix, "gitlab")'
    And the generated "cmd/feattool/forge.go" file does not contain '"github"'

  Scenario: One provider is its own default and ships as the tool's embedded defaults
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude-local"
    Then the project exit code is 0
    And the project manifest contains "default:"
    And the project manifest contains "provider: claude-local"
    And the generated "cmd/feattool/chat/assets/config.yaml" file contains "claude-local"
    And the generated "cmd/feattool/chat/assets/init/config.yaml" file contains "claude-local"
    And the generated "cmd/feattool/chat.go" file contains "setup.RegisterAssets(props.AiCmd"

  Scenario: Several providers need the author to name the default
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude,openai"
    Then the project exit code is not zero
    And the project output contains "--chat-default-provider"

  Scenario: A named default is recorded and must be one of the linked providers
    Given I generate a gtb project with features "init,update,ai", chat providers "claude,openai" and chat default "openai"
    Then the project exit code is 0
    And the project manifest contains "provider: openai"
    And the generated "cmd/feattool/chat/assets/config.yaml" file contains "openai"
    And the generated "cmd/feattool/chat/assets/config.yaml" file does not contain "claude"

  Scenario: A default outside the linked providers is refused
    Given I generate a gtb project with features "init,update,ai", chat providers "claude,openai" and chat default "gemini"
    Then the project exit code is not zero
    And the project output contains "not one the tool links"

  Scenario: An OpenAI-compatible default needs its endpoint
    Given I generate a gtb project with features "init,update,ai" and chat providers "openai-compatible"
    Then the project exit code is not zero
    And the project output contains "--chat-base-url"

  Scenario: An OpenAI-compatible default with an endpoint ships it in the defaults
    Given I generate a gtb project with features "init,update,ai", chat providers "openai-compatible" and chat base URL "https://llm.internal/v1"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat/assets/config.yaml" file contains "https://llm.internal/v1"

  Scenario: A deleted defaults bundle comes back on regenerate
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude-local"
    Then the project exit code is 0
    When I delete the generated "cmd/feattool/chat/assets/config.yaml" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat/assets/config.yaml" file contains "claude-local"

  Scenario: Deleted adapter files come back on regenerate, and only the manifest ships none
    Given I generate a gtb project with features "init,update,ai" and chat providers "claude-local"
    Then the project exit code is 0
    When I delete the generated "cmd/feattool/chat.go" file
    And I delete the generated "cmd/feattool/forge.go" file
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains "gitlab.com/phpboyscout/go/chat-anthropic"
    And the generated "cmd/feattool/forge.go" file contains "gitlab.com/phpboyscout/go/forge-github"
    When I set the project manifest chat providers to none
    And I run gtb in the project with "regenerate project --overwrite allow"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file exists
    And the generated "cmd/feattool/chat.go" file does not contain "gitlab.com/phpboyscout/go/chat-"

  Scenario: Credential forges link their adapters beside the backend's, and codeberg shares gitea's
    Given I generate a gtb project with forge backend "github" and forge credentials "codeberg"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains "gitlab.com/phpboyscout/go/forge-github"
    And the generated "cmd/feattool/forge.go" file contains "gitlab.com/phpboyscout/go/forge-gitea"
    And the generated "cmd/feattool/forge.go" file does not contain "forge-gitlab"
    And the project manifest contains "type: github"
    And the project manifest contains "- codeberg"

  Scenario: The ai feature with no provider is refused at generation
    Given I generate a gtb project with features "init,update,ai" and chat providers ""
    Then the project exit code is not zero
    And the project output contains "at least one chat provider"

  Scenario: A provider no module registers is refused at generation
    Given I generate a gtb project with features "init,update,ai" and chat providers "chatgpt"
    Then the project exit code is not zero
    And the project output contains "not one a known module registers"

  Scenario: Enabling ai is one command, and records the default providers
    Given a freshly generated gtb project
    When I run gtb in the project with "enable ai"
    Then the project exit code is 0
    And the project manifest contains "- claude-local"
    And the project manifest contains "- openai-compatible"
    And the generated "cmd/feattool/chat.go" file contains "chat-anthropic"
    And the generated "cmd/feattool/chat.go" file contains "chat-openai"
    And the generated "cmd/feattool/chat.go" file contains "chat-gemini"
    And the project output contains "names no default"
    And the generated "cmd/feattool/chat/assets/config.yaml" file does not exist
    When I run gtb in the project with "disable ai"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file contains "chat-anthropic"
    And the project output contains "still links"
    When I run gtb in the project with "unset chat.providers"
    Then the project exit code is 0
    And the generated "cmd/feattool/chat.go" file does not exist

  Scenario: A project on no forge has no forge adapter file
    Given I generate a gtb project with the flags "--no-forge --module example.internal/feattool --features init,docs"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file does not exist
    And the generated "cmd/feattool/chat.go" file does not exist

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

  Scenario: Disabling a credential forge removes its adapter on regenerate, go.mod included
    Given I generate a gtb project with forge backend "gitlab" and forge credentials "github"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file contains "forge-github"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-github v"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-gitlab v"
    When I run gtb in the project with "disable github"
    Then the project exit code is 0
    When I run gtb in the project with "regenerate project"
    Then the project exit code is 0
    And the generated "cmd/feattool/forge.go" file does not contain "forge-github"
    And the generated "cmd/feattool/forge.go" file contains "forge-gitlab"
    And the generated "go.mod" file does not contain "forge-github"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-gitlab v"

  Scenario: A machine without Go gets a correct go.mod and a stated reason, not an exec error
    Given I generate a gtb project with forge backend "gitlab" and forge credentials "github"
    Then the project exit code is 0
    When I run gtb in the project with "disable github" and no Go toolchain on PATH
    Then the project exit code is 0
    And the generated "go.mod" file does not contain "forge-github"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-gitlab v"
    And the project output contains "not verified: no Go toolchain on PATH"
    And the project output does not contain "executable file not found"
    When I run gtb in the project with "regenerate project --overwrite allow" and no Go toolchain on PATH
    Then the project exit code is 3
    And the project output contains "not verified: no Go toolchain on PATH"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-gitlab v"

  Scenario: A regenerate without verification keeps every requirement
    Given I generate a gtb project with forge backend "gitlab" and forge credentials "github"
    Then the project exit code is 0
    And the generated "go.mod" file contains "// indirect"
    When I run gtb in the project with "regenerate project --overwrite allow --no-verify"
    Then the project exit code is 0
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-github v"
    And the generated "go.mod" file contains "gitlab.com/phpboyscout/go/forge-gitlab v"
    And the generated "go.mod" file contains "// indirect"

  Scenario: The generated go.mod names no gtb or linter tool line, and the README says how to install gtb
    Given a freshly generated gtb project
    Then the generated "go.mod" file does not contain "cli/cmd/gtb"
    And the generated "go.mod" file does not contain "golangci-lint"
    And the generated "README.md" file contains "go install gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb@"
