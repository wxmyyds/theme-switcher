package config

import (
	"encoding/json"
	"os"
)

const filePerm = 0644

type Config struct {
	LightModeWhiteText bool `json:"light_mode_white_text"`
	DarkModeWhiteText  bool `json:"dark_mode_white_text"`
	LightTimeStart     int  `json:"light_time_start"`
	DarkTimeStart      int  `json:"dark_time_start"`
	EnableLogging      bool `json:"enable_logging"`
}

var Default = Config{
	LightModeWhiteText: false,
	DarkModeWhiteText:  true,
	LightTimeStart:     6,
	DarkTimeStart:      18,
	EnableLogging:      true,
}

func (c *Config) Validate() {
	if c.LightTimeStart < 0 || c.LightTimeStart > 23 {
		c.LightTimeStart = Default.LightTimeStart
	}
	if c.DarkTimeStart < 0 || c.DarkTimeStart > 23 {
		c.DarkTimeStart = Default.DarkTimeStart
	}
}

func Load(path string) (*Config, error) {
	cfg := Default
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, Save(path, &cfg)
		}
		return &cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		cfg = Default
		return &cfg, Save(path, &cfg)
	}
	cfg.Validate()
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, filePerm)
}