package web

type Config struct {
	Port            uint16 `yaml:"port" envconfig:"PORT"`
	BaseUrl         string `yaml:"baseUrl" envconfig:"BASE_URL"`
	ReloadTemplates bool   `yaml:"reloadTemplates" envconfig:"RELOAD_TEMPLATES"`
}

func NewConfig() *Config {
	return &Config{
		Port:            8080,
		BaseUrl:         "",
		ReloadTemplates: false,
	}
}
