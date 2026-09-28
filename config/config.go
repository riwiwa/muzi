package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"

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
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Environment variables override the config file, which suits containers
var envOverrides = map[string]func(c *Config) *string{
	"MUZI_ADDRESS":     func(c *Config) *string { return &c.Server.Address },
	"MUZI_PUBLIC_URL":  func(c *Config) *string { return &c.Server.PublicUrl },
	"MUZI_DB_HOST":     func(c *Config) *string { return &c.Database.Host },
	"MUZI_DB_PORT":     func(c *Config) *string { return &c.Database.Port },
	"MUZI_DB_USER":     func(c *Config) *string { return &c.Database.User },
	"MUZI_DB_PASSWORD": func(c *Config) *string { return &c.Database.Password },
	"MUZI_DB_NAME":     func(c *Config) *string { return &c.Database.Name },
	"MUZI_UPLOADS_DIR": func(c *Config) *string { return &c.Storage.UploadsDir },
}

func applyEnv(c *Config) error {
	for name, field := range envOverrides {
		if v, ok := os.LookupEnv(name); ok {
			*field(c) = v
		}
	}
	for name, field := range map[string]*bool{
		"MUZI_ALLOW_SIGNUP":      &c.Server.AllowSignup,
		"MUZI_IMAGES_AUTO_FETCH": &c.Images.AutoFetch,
	} {
		if v, ok := os.LookupEnv(name); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s must be true or false, got %q", name, v)
			}
			*field = b
		}
	}
	return nil
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

// Connection URL for the database (or just the server, to create the database). The user and
// password are escaped, so passwords with characters like @ or / work.
func (d *DatabaseConfig) GetDbUrl(withDb bool) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   net.JoinHostPort(d.Host, d.Port),
	}
	if withDb {
		u.Path = "/" + d.Name
	}
	return u.String()
}
