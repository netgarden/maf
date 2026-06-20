package logging

type Config struct {
	Level               string `yaml:"level" envconfig:"LEVEL"`
	Format              string `yaml:"format" envconfig:"FORMAT"`
	Destination         string `yaml:"destination" envconfig:"DESTINATION"`
	DestinationFilepath string `yaml:"destination_filepath" envconfig:"DESTINATION_FILEPATH"`
}

func NewConfig() *Config {
	return &Config{
		Level:       "debug",
		Format:      "text",
		Destination: "stdout",
	}
}
