package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Images   ImagesConfig
	Storage  StorageConfig
}

type StorageConfig struct {
	// Where uploaded images are saved; relative paths are relative to the working directory
	UploadsDir string `toml:"uploads_dir"`
}

type ServerConfig struct {
	Address string
	// Externally visible base URL, e.g. "https://muzi.example.com"; used for the Spotify redirect URI.
	// When empty it's derived from each request.
	PublicUrl string `toml:"public_url"`
	// Let anyone who can reach the server create an account. The first account can always be
	// created; after that signup is closed unless this is set.
	AllowSignup bool `toml:"allow_signup"`
}

type ImagesConfig struct {
	// Automatically fetch missing artist/album/song images from Spotify and Deezer
	AutoFetch bool `toml:"auto_fetch"`
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

var cfg *Config

// The config file to read; set with SetPath before LoadConfig
var path = "config.toml"

// Sets which config file LoadConfig reads (muzi's -config flag)
func SetPath(p string) {
	path = p
}

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
		Images: ImagesConfig{
			AutoFetch: true,
		},
		Storage: StorageConfig{
			UploadsDir: "static/uploads",
		},
	}

	if _, err := os.Stat(path); err == nil {
		_, err := toml.DecodeFile(path, cfg)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %w", path, err)
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
