package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
}

type ServerConfig struct {
	Address string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

var cfg *Config

func LoadConfig() (*Config, error) {
	cfg = &Config{
		Server: ServerConfig{
			Address: "0.0.0.0:1234",
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     "5432",
			User:     "postgres",
			Password: "postgres",
			Name:     "muzi",
		},
	}

	if _, err := os.Stat("config.toml"); err == nil {
		_, err := toml.DecodeFile("config.toml", cfg)
		if err != nil {
			return nil, fmt.Errorf("error parsing config.toml: %w", err)
		}
	}

	return cfg, nil
}

func Get() *Config {
	if cfg == nil {
		var err error
		cfg, err = LoadConfig()
		if err != nil {
			panic(fmt.Sprintf("failed to load config: %v", err))
		}
	}
	return cfg
}

func (d *DatabaseConfig) GetDbUrl(withDb bool) string {
	if withDb {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
			d.User, d.Password, d.Host, d.Port, d.Name)
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s",
		d.User, d.Password, d.Host, d.Port)
}
