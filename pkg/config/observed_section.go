package config

import (
	"sync"

	"github.com/cockroachdb/errors"
)

// ObservedSection stores the latest typed snapshot of an observed config
// section.
type ObservedSection[T any] struct {
	mu      sync.RWMutex
	current *Section[T]
}

// Value returns the latest complete settings snapshot.
func (s *ObservedSection[T]) Value() T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.current == nil {
		var zero T

		return zero
	}

	return s.current.Value
}

// Current returns a pointer to the latest immutable settings snapshot. Callers
// must treat the returned value as read-only and call Current again when they
// need to observe a later reload.
func (s *ObservedSection[T]) Current() *T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.current == nil {
		return nil
	}

	return &s.current.Value
}

// Exists reports whether the latest snapshot came from an explicit section.
func (s *ObservedSection[T]) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.current != nil && s.current.Exists
}

func (s *ObservedSection[T]) store(section Section[T]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := section
	s.current = &next
}

// SectionBindingOption customises ObserveSection behaviour.
type SectionBindingOption[T any] func(*SectionBindingConfig[T])

// SectionBindingConfig holds ObserveSection option state.
type SectionBindingConfig[T any] struct {
	defaults    T
	hasDefaults bool
	merge       func(defaults, overlay T) T
	validate    func(T) error
	apply       func(Section[T]) error
}

// WithSectionDefaults starts each observed section from defaults and merges the
// decoded section over it when the section exists.
func WithSectionDefaults[T any](defaults T, merge func(defaults, overlay T) T) SectionBindingOption[T] {
	return func(cfg *SectionBindingConfig[T]) {
		cfg.defaults = defaults
		cfg.hasDefaults = true
		cfg.merge = merge
	}
}

// WithSectionValidator validates each settings snapshot before it is published.
func WithSectionValidator[T any](validate func(T) error) SectionBindingOption[T] {
	return func(cfg *SectionBindingConfig[T]) {
		cfg.validate = validate
	}
}

// WithSectionApply registers a callback that runs after a successful rehydrate.
func WithSectionApply[T any](apply func(Section[T]) error) SectionBindingOption[T] {
	return func(cfg *SectionBindingConfig[T]) {
		cfg.apply = apply
	}
}

// ObserveSection unmarshals an initial typed section snapshot and registers a
// config observer that rehydrates the snapshot on successful config reloads.
func ObserveSection[T any](
	cfg Containable,
	key string,
	opts ...SectionBindingOption[T],
) (*ObservedSection[T], error) {
	settings := SectionBindingConfig[T]{}

	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}

	observed := &ObservedSection[T]{}

	initial, err := loadObservedSection(cfg, key, settings)
	if err != nil {
		return nil, err
	}

	observed.store(initial)

	if cfg != nil {
		cfg.AddObserverFunc(func(next Containable) error {
			section, err := loadObservedSection(next, key, settings)
			if err != nil {
				return err
			}

			observed.store(section)

			if settings.apply != nil {
				return settings.apply(section)
			}

			return nil
		})
	}

	return observed, nil
}

func loadObservedSection[T any](
	cfg Containable,
	key string,
	settings SectionBindingConfig[T],
) (Section[T], error) {
	section, err := UnmarshalSection[T](cfg, key)
	if err != nil {
		return Section[T]{}, err
	}

	if settings.hasDefaults {
		if section.Exists {
			if settings.merge == nil {
				return Section[T]{}, errors.New("section defaults require a merge function")
			}

			section.Value = settings.merge(settings.defaults, section.Value)
		} else {
			section.Value = settings.defaults
		}
	}

	if settings.validate != nil {
		if err := settings.validate(section.Value); err != nil {
			return Section[T]{}, err
		}
	}

	return section, nil
}
