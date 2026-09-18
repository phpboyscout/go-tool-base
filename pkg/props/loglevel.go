package props

import "log/slog"

// GetLogLevel returns the level var the root moves, creating it on a Props
// nobody defaulted so every reader shares one. A nil Props answers with a var
// nothing will ever move.
func (p *Props) GetLogLevel() *slog.LevelVar {
	if p == nil {
		return &slog.LevelVar{}
	}

	if p.LogLevel == nil {
		p.LogLevel = &slog.LevelVar{}
	}

	return p.LogLevel
}
