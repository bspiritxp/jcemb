package app

import "github.com/bspiritxp/jcemb/internal/config"

type Bootstrap struct {
	Config config.RuntimeConfig
	Err    error
}

func NewBootstrap() Bootstrap {
	loaded, err := config.Load()
	if err != nil {
		defaults := config.Defaults()
		return Bootstrap{
			Config: config.RuntimeConfig{
				Path:     defaults.ConfigFile,
				Settings: defaults.Global,
			},
			Err: err,
		}
	}

	return Bootstrap{Config: loaded}
}

func (b Bootstrap) Validate() error {
	return b.Err
}
