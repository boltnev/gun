package gun

type Gun struct {
}

func NewGun() *Gun {
	return &Gun{}
}

func (g *Gun) Run() error {
	return nil
}
