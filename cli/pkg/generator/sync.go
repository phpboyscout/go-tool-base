package generator

// syncDerivedFromManifest is the one function that brings a project's
// generated files into line with its manifest: the derived fields an older
// manifest lacks, the root command, the entry point, version package and
// generate directives beside it (#84), the signing-owned files and the adapter
// files (chat, forge, the framework links, the chat defaults bundle). Every writer of
// the manifest runs it (regenerate, enable/disable, enable signing, attach),
// so no command leaves a tree that needs a regenerate to build (spec 0197
// D7).
//
// The derived fields are recorded before anything renders, so the render
// reads them and a later hash persistence that re-reads the manifest from
// disk keeps them.
func (g *Generator) syncDerivedFromManifest(m *Manifest) error {
	if err := g.syncDerivedManifestFields(m); err != nil {
		return err
	}

	g.warnAboutManifest(m)

	if err := g.regenerateRootCommand(*m); err != nil {
		return err
	}

	if err := g.syncSkeletonGoFiles(*m); err != nil {
		return err
	}

	if err := g.syncSigningFiles(*m); err != nil {
		return err
	}

	if err := g.syncAdapterFiles(m); err != nil {
		return err
	}

	// After every generated Go file is on disk, so the imports the seed reads
	// are this run's (spec 0200 D2).
	return g.seedGoMod(g.config.Path, manifestModulePath(*m), resolveGoVersion(m.Version.Go), g.currentVersion())
}

// warnAboutManifest logs what ManifestWarnings has to say, once per run, on
// every path that writes the manifest.
func (g *Generator) warnAboutManifest(m *Manifest) {
	for _, w := range ManifestWarnings(m) {
		g.props.Logger.Warn(w)
	}
}
