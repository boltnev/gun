package gun

type Gun struct{

}

type Config struct{

}

func NewGun(cfg *Config) *Gun{
	return &Gun{}
}

func (g *Gun) Prepare() error{
	return nil
}

func (g *Gun) Run() error{
	return nil
}
