---
title: Coming from Other Ecosystems
description: A guide for developers migrating to Go Tool Base from PHP, Ruby, or Python frameworks.
date: 2026-03-23
tags: [concepts, migration, onboarding]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Coming from Other Ecosystems?

If you are transitioning to Go from a highly productive, convention-driven ecosystem like **PHP (Laravel, Silverstripe)**, **Ruby (Rails)**, or **Python (Django)**, you have likely experienced the "onboarding gap."

In those frameworks, a `scaffold` or `make` command instantly provides authentication, configuration, and structural sanity. In idiomatic Go, developers are traditionally expected to hand-pick every library—a router, a config parser, an injection strategy—and wire them manually.

**Go Tool Base (GTB)** bridges this gap. It provides the **Convention-over-Configuration** experience you are used to, without sacrificing Go's raw performance, strict typing, or composability.

---

## 🗺️ Concept Mapping

Here is how common concepts from RAD frameworks map to GTB's architecture:

| Your Ecosystem | GTB Equivalent | Description |
| :--- | :--- | :--- |
| `artisan make:command` / `rails g` | `gtb generate command` | Automatically scaffolds new CLI commands with tests and wiring. |
| Service Container / DI | The `Props` struct | Provides global access to config, logger, filesystem, and AI clients, passed explicitly to every command constructor. |
| `.env` & `config/app.php` | The `Config` Container | A unified configuration system (wrapping Viper) that seamlessly merges environment variables, YAML files, and embedded defaults. |
| `php artisan serve` | Custom commands | Add your own `serve` command to start an HTTP server using the GTB architecture. |
| Framework "Magic" | Code Generation | Instead of slow runtime reflection, GTB uses generators (`gtb generate skeleton`) to produce explicitly typed boilerplate code at compile time. |

---

## The "Micro-RAD" Approach

Unlike monolithic Go frameworks (like Buffalo or Beego) which lock you into proprietary ORMs or templating engines, GTB acts as a "Micro-RAD" toolkit:

1. **Opinionated Structure, Unopinionated Logic:** GTB dictates *where* your config goes and *how* your commands are wired, but it absolutely does not dictate how you write your SQL queries or format your JSON.
2. **The "Eject Path":** Because GTB heavily utilizes code generation and standard interfaces (`fs.FS`, `io.Writer`), the code it produces is idiomatic Go. If your enterprise outgrows GTB, you aren't trapped in a proprietary abstraction layer.

Welcome to Go without the boilerplate fatigue.
