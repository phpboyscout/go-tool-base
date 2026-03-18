package generator

var CalculateHash = calculateHash
var VerifyHash = (*Generator).verifyHash

func (g *Generator) RegisterSubcommand() error {
	return g.registerSubcommand()
}

func (g *Generator) DeregisterSubcommand() error {
	return g.deregisterSubcommand()
}
