---
title: Build an AI-backed command
description: Add a command that calls a language model through the chat client, with the rules in a system prompt, the mechanics in Go tools the model must call, the state in a struct you own, and each turn returned as a typed value.
date: 2026-10-30
tags: [tutorial, ai, chat, tools, cli]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Build an AI-backed command

By the end of this, `mytool adventure` runs a short dungeon game in your
terminal, narrated by a model acting as dungeon master. The game is the excuse.
What you're building is the shape of an AI feature you can trust: the model
writes the prose, and your code owns everything that has to be right.

- **The rules** live in the system prompt.
- **The mechanics** live in Go functions the model calls as tools. It never
  makes up a dice roll.
- **The state** lives in a struct you hand the model fresh every turn, so it
  never has to remember the fight.
- **Each turn** comes back as a typed Go value, never prose to parse.

Allow about thirty minutes, plus however long you spend in the cave.

## Before you start

- **The `mytool` project** from [Build your first CLI](build-your-first-cli.md).
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **An Anthropic API key.** Every turn of the game makes paid API calls. Other
  providers work too, and the [AI integration guide](../how-to/ai-integration.md)
  lists them.

## Link a provider

The chat client is a library your code imports. Each provider is a separate
module, and your tool links only the ones you name:

```bash
gtb set chat.providers claude
```

That writes `cmd/mytool/chat.go`, which blank-imports the Anthropic module, and
adds it to `go.mod`. No regenerate is needed.

The client looks for the key in your config first and then in the provider's
usual variable. For playing locally, the variable is enough:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

For a tool you ship, the `ai` feature gives your users an `init` step that
stores the key properly. See the [AI integration guide](../how-to/ai-integration.md).

## Scaffold the command

```bash
gtb generate command --name adventure --short "Play a dungeon adventure"
```

Everything below goes in `pkg/cmd/adventure/`, in files of your own beside the
generated `cmd.go`.

## Own the state

Create `pkg/cmd/adventure/game.go`. The game state is a plain struct, and the
turn is the shape the model has to hand back:

```go
package adventure

import (
	"fmt"
	"strings"
)

// GameState is the single source of truth for the game. Extend it freely.
type GameState struct {
	PlayerHP  int
	Location  string
	Inventory []string
	Foes      map[string]int // foe name -> remaining hit points
}

// summary renders the state into the line the model is given each turn.
func (g *GameState) summary() string {
	foes := make([]string, 0, len(g.Foes))
	for name, hp := range g.Foes {
		foes = append(foes, fmt.Sprintf("%s (%d HP)", name, hp))
	}

	return fmt.Sprintf("You have %d HP, at %s, carrying %s. Foes: %s.",
		g.PlayerHP, g.Location, strings.Join(g.Inventory, ", "), strings.Join(foes, ", "))
}

// Turn is what the dungeon master hands back at the end of every turn.
type Turn struct {
	Narration string   `json:"narration" jsonschema_description:"two-sentence narration of what just happened"`
	Choices   []string `json:"choices" jsonschema_description:"the actions the player may take next"`
	GameOver  bool     `json:"game_over" jsonschema_description:"true only if the game has ended"`
}
```

The model never holds the state. You give it the current truth at the start of
every turn, so it can't quietly rewrite what happened.

## Give the dungeon master its tools

A tool is a name, a description the model reads to decide when to use it, a
JSON Schema for its arguments, and a handler. `chat.GenerateSchema` builds the
schema from a tagged struct.

Create `pkg/cmd/adventure/tools.go`:

```go
package adventure

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"

	"gitlab.com/phpboyscout/go/chat"
)

const (
	defaultSides = 20 // a d20, when the model names no die
	toHit        = 10 // an attack roll below this misses
	weaponSides  = 6  // a short sword does 1d6
)

// roll returns 1..sides. crypto/rand is more than a die needs, but it keeps
// the scaffold's security linter quiet without an exception.
func roll(sides int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(sides)))
	if err != nil {
		return 0, err
	}

	return int(n.Int64()) + 1, nil
}

// RollParams is what the model passes to the roll tool.
type RollParams struct {
	Sides int `json:"sides" jsonschema_description:"number of sides on the die"`
}

func rollTool() chat.Tool {
	return chat.Tool{
		Name:        "roll",
		Description: "Roll a die with the given number of sides; returns 1..sides.",
		Parameters:  chat.GenerateSchema[RollParams](),
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var p RollParams
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			if p.Sides <= 0 {
				p.Sides = defaultSides
			}

			return roll(p.Sides)
		},
	}
}

// AttackParams is what the model passes to the attack tool.
type AttackParams struct {
	Target string `json:"target" jsonschema_description:"the name of the foe being attacked"`
}

func attackTool(game *GameState) chat.Tool {
	return chat.Tool{
		Name:        "attack",
		Description: "Resolve the player's attack on a named foe. Rolls to hit, applies damage.",
		Parameters:  chat.GenerateSchema[AttackParams](),
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var p AttackParams
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, err
			}

			hp, ok := game.Foes[p.Target]
			if !ok {
				return map[string]any{"error": "no such foe: " + p.Target}, nil
			}

			attack, err := roll(defaultSides)
			if err != nil {
				return nil, err
			}

			if attack < toHit {
				return map[string]any{"hit": false, "foe": p.Target}, nil
			}

			damage, err := roll(weaponSides)
			if err != nil {
				return nil, err
			}

			hp = max(hp-damage, 0)
			game.Foes[p.Target] = hp

			return map[string]any{
				"hit": true, "foe": p.Target, "damage": damage,
				"foe_hp": hp, "defeated": hp == 0,
			}, nil
		},
	}
}

func submitTurnTool(out *Turn) chat.Tool {
	return chat.Tool{
		Name:        "submit_turn",
		Description: "End your turn. Call this exactly once, last, with the turn's outcome.",
		Parameters:  chat.GenerateSchema[Turn](),
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			if err := json.Unmarshal(args, out); err != nil {
				return nil, err
			}

			return "turn recorded", nil
		},
	}
}
```

Three things to notice:

- **`attack` changes the state.** It looks the foe up by name, so it works for
  any creature you add later. A bad name comes back as an error value the model
  can read and recover from.
- **The turn is a tool too.** A single call can run tools or return a typed
  value, not both. So the model ends each turn by calling `submit_turn`, and its
  handler fills in your `Turn`. The schema enforces the shape.
- **The scaffold's linter is strict.** It rejects `math/rand` and unnamed
  numbers, which is why the dice use `crypto/rand` and named constants. Keep
  them, or `gtb regenerate project` fails its lint step.

## Write the rules and the loop

Replace `pkg/cmd/adventure/main.go`:

```go
package adventure

import (
	"context"
	"fmt"

	"charm.land/huh/v2"
	"gitlab.com/phpboyscout/go/chat"

	gtbchat "gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

const (
	startingHP = 20
	goblinHP   = 12
	// maxSteps bounds the tool calls in one turn: a roll or an attack, then
	// submit_turn, with room for the model to change its mind.
	maxSteps = 8
)

const dmRules = `You are the dungeon master of a short fantasy adventure. Each turn
you are given the current game state and the player's action.

Resolve the action and end the turn:
- If the player attacks, you MUST call the attack tool with the foe's name to
  resolve it. Do not decide the hit or the damage yourself.
- For any other chance event, call the roll tool and use its result.
- For simple actions, just narrate them.
- Then call submit_turn exactly once: a two-sentence narration, two or three
  choices, and game_over.

Trust the state you are given; never contradict it. A foe at 0 hit points is dead
and stays dead. The game ends when the player's hit points reach 0 (they lose), or
when the player reaches a satisfying ending. When it ends, set game_over and narrate
the finish.

Keep the tone light and quick.`

func RunAdventure(ctx context.Context, p *props.Props, _ *AdventureOptions, _ []string) error {
	game := &GameState{
		PlayerHP:  startingHP,
		Location:  "the mouth of a damp cave",
		Inventory: []string{"a short sword", "a guttering torch"},
		Foes:      map[string]int{"goblin": goblinHP},
	}

	var turn Turn

	client, err := gtbchat.NewFromProps(ctx, p, chat.Config{
		SystemPrompt: dmRules,
		MaxSteps:     maxSteps,
	})
	if err != nil {
		return err
	}

	if err := client.SetTools([]chat.Tool{
		rollTool(),
		attackTool(game),
		submitTurnTool(&turn),
	}); err != nil {
		return err
	}

	action := "I step into the cave."

	for {
		turn = Turn{}

		// Hand the model the current truth, then the player's action.
		input := fmt.Sprintf("State: %s\nThe player: %s", game.summary(), action)
		if _, err := client.Chat(ctx, input); err != nil {
			return err
		}

		fmt.Println("\n" + turn.Narration)

		if turn.GameOver {
			return nil
		}

		action, err = chooseAction(ctx, p, turn.Choices)
		if err != nil {
			return err
		}
	}
}

func chooseAction(ctx context.Context, p *props.Props, choices []string) (string, error) {
	const other = "__other__"

	options := make([]huh.Option[string], 0, len(choices)+1)
	for _, c := range choices {
		options = append(options, huh.NewOption(c, c))
	}

	options = append(options, huh.NewOption("Something else...", other))

	var pick, custom string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you do?").
				Options(options...).
				Value(&pick),
		),
		// A second step that only appears when the player chose "Something else".
		huh.NewGroup(
			huh.NewInput().
				Title("Describe your action").
				Value(&custom),
		).WithHideFunc(func() bool { return pick != other }),
	)

	if err := setup.RunForm(ctx, p, form); err != nil {
		return "", err
	}

	if pick == other {
		return custom, nil
	}

	return pick, nil
}
```

Two lines of the rules do the heavy lifting. "You MUST call the attack tool"
stops the model deciding hits and damage itself. "Trust the state you are given"
keeps the world consistent, because the state line you send each turn is always
current: `attack` changed `game` last turn.

The same client runs every turn, so the conversation carries through the game.
`MaxSteps` caps the tool calls in one turn. Running out is an error, not a
truncated answer, so keep it small but not tight.

"Something else..." is always on the menu. Pick it and you can type any action
you like. The model handles it within the rules you gave it.

## Play it

```bash
just build
./bin/mytool adventure
```

Each turn prints the model's narration and a menu of choices. The wording
changes every run. The mechanics don't: every hit, miss and hit-point change
comes from your `attack` tool and lands in your `GameState`.

Without a key, the command stops before the first turn:

```
ERROR unable to construct client: Anthropic API key is required but not provided
```

## Where this leaves you

An AI feature you can ship keeps the model away from everything that has to be
right. The rules are in the prompt, the mechanics are in tools the model must
call, the state is in a struct you own, and the output is a value your code
can trust. The model's unpredictability is confined to the wording.

The prose is never reproducible. `chat.Config.Temperature` lowers the variance
between runs, but doesn't make the output deterministic. What you can make
reproducible is the mechanics, and for most features that's what you needed.

To grow the game, add tools: a `move` that updates `Location`, a `use_item` that
reaches into `Inventory`, a second creature in `Foes`. The loop doesn't change.

## What to read next

- [Let the AI call your Go functions](../how-to/ai-tool-calling.md): the tool
  loop in detail.
- [Structured AI responses](../how-to/structured-ai-responses.md): typed answers
  without tools.
- [Persist chat conversations](../how-to/persist-chat-conversations.md): save
  and resume a game.
