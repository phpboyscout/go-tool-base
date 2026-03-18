package config

type Observable interface {
	Run(Containable, chan error)
}

type Observer struct {
	handler func(Containable, chan error)
}

func (o Observer) Run(c Containable, errs chan error) {
	o.handler(c, errs)
}
