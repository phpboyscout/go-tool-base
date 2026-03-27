package changelog

// Category classifies a change entry.
type Category int

const (
	// CategoryBreaking represents a breaking change requiring user action.
	CategoryBreaking Category = iota
	// CategoryFeature represents a new feature.
	CategoryFeature
	// CategoryFix represents a bug fix.
	CategoryFix
	// CategoryPerformance represents a performance improvement.
	CategoryPerformance
	// CategoryOther represents any other change (refactor, docs, chore, etc.).
	CategoryOther
)

// Entry represents a single change within a release.
type Entry struct {
	// Category is the type of change.
	Category Category
	// Scope is the conventional commit scope (e.g., "http", "chat"). May be empty.
	Scope string
	// Description is the change description text.
	Description string
	// Raw is the original unparsed line from the release notes.
	Raw string
}

// Release represents the parsed changelog for a single release version.
type Release struct {
	// Version is the release tag (e.g., "v1.5.0").
	Version string
	// Entries contains all parsed change entries for this release.
	Entries []Entry
}

// Changelog represents the parsed changelog across multiple releases.
type Changelog struct {
	// FromVersion is the starting version (exclusive).
	FromVersion string
	// ToVersion is the ending version (inclusive).
	ToVersion string
	// Releases contains parsed releases ordered from oldest to newest.
	Releases []Release
}

// HasBreakingChanges returns true if any release contains breaking changes.
func (c *Changelog) HasBreakingChanges() bool {
	for _, r := range c.Releases {
		for _, e := range r.Entries {
			if e.Category == CategoryBreaking {
				return true
			}
		}
	}

	return false
}

// BreakingChanges returns all breaking change entries across all releases.
func (c *Changelog) BreakingChanges() []Entry {
	return c.EntriesByCategory(CategoryBreaking)
}

// EntriesByCategory returns all entries matching the given category across all releases.
func (c *Changelog) EntriesByCategory(cat Category) []Entry {
	var entries []Entry

	for _, r := range c.Releases {
		for _, e := range r.Entries {
			if e.Category == cat {
				entries = append(entries, e)
			}
		}
	}

	return entries
}
