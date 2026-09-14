package chat

// No chat provider is registered here. Registration is a blank import in the
// binary that ships the provider (spec 0194 D3): gtb's own main links every
// module, and a generated tool links the ones its manifest selects. See
// ProviderModule for which module registers which provider.
