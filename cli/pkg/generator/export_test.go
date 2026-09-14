package generator

var CalculateHash = calculateHash
var ResolveCommandFileConflict = (*Generator).resolveCommandFileConflict

func (g *Generator) RegisterSubcommand() error {
	return g.registerSubcommand()
}

func (g *Generator) DeregisterSubcommand() error {
	return g.deregisterSubcommand()
}
