# Root Command

The entry point and orchestration layer for GTB CLI applications.

Key Responsibilities:
- Persistent service initialization (Logging, Configuration)
- Global flag management (`--config`, `--debug`, `--ci`)
- Lifecycle hooks (`PersistentPreRunE`)
- Automatic feature command registration

For detailed documentation on the root command and the application lifecycle, see the **[Built-in Commands Documentation](../../docs/components/commands/root.md)**.
