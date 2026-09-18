package generator

var CalculateHash = calculateHash
var ResolveCommandFileConflict = (*Generator).resolveCommandFileConflict

func (g *Generator) RegisterSubcommand() error {
	_, err := g.registerSubcommand()

	return err
}

func (g *Generator) DeregisterSubcommand() error {
	return g.deregisterSubcommand()
}
