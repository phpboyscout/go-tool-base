// Package bitbucket provides a release.Provider implementation for Bitbucket
// Cloud using the Downloads API. Bitbucket has no native "Releases" concept;
// version information is inferred from asset filenames using a configurable
// regular expression. Provider construction uses package-owned [Settings]; GTB
// config integration lives in [SettingsFromConfig].
package bitbucket
