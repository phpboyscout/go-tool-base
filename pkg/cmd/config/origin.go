package config

import (
	cfg "gitlab.com/phpboyscout/go/config"
)

// fileBacked reports whether any file layer defines key.
func fileBacked(view *cfg.View, key string) bool {
	if origin, ok := view.Origin(key); ok && origin.Kind == cfg.SourceFile {
		return true
	}

	for _, source := range view.Shadowed(key) {
		if source.Kind == cfg.SourceFile {
			return true
		}
	}

	return false
}

// userAuthoredKey reports whether any user-influenced layer — file, env or
// flag — defines key, as opposed to the embedded defaults shipped with the
// tool, which the user cannot edit away.
func userAuthoredKey(view *cfg.View, key string) bool {
	isUser := func(s cfg.Source) bool {
		return s.Kind == cfg.SourceFile || s.Kind == cfg.SourceEnv || s.Kind == cfg.SourceFlag
	}

	if origin, ok := view.Origin(key); ok && isUser(origin) {
		return true
	}

	for _, source := range view.Shadowed(key) {
		if isUser(source) {
			return true
		}
	}

	return false
}
