package app

type Bootstrap struct {
	Name string
}

func NewBootstrap() Bootstrap {
	return Bootstrap{Name: "bootstrap"}
}
