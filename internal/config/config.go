package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds lazytunnel server configuration.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type ServerConfig struct {
	Addr    string     `mapstructure:"addr"`
	TLSCert string     `mapstructure:"tls_cert"`
	TLSKey  string     `mapstructure:"tls_key"`
	CORS    CORSConfig `mapstructure:"cors"`
}

type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type AuthConfig struct {
	JWTSecret          string        `mapstructure:"jwt_secret"`
	JWTSecretEnv       string        `mapstructure:"jwt_secret_env"`
	TokenExpiration    time.Duration `mapstructure:"token_expiration"`
	AutoStartTunnels   bool          `mapstructure:"auto_start_tunnels"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Load reads configuration from file, environment, and applies flag overrides.
func Load(configPath string, overrides map[string]interface{}) (*Config, error) {
	v := viper.New()

	v.SetDefault("server.addr", ":8080")
	v.SetDefault("database.path", "tunnels.db")
	v.SetDefault("auth.jwt_secret_env", "LAZYTUNNEL_JWT_SECRET")
	v.SetDefault("auth.token_expiration", "24h")
	v.SetDefault("auth.auto_start_tunnels", false)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")
	v.SetDefault("server.cors.allowed_origins", []string{"*"})

	v.SetEnvPrefix("LAZYTUNNEL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	for key, value := range overrides {
		if value != nil && value != "" && value != false {
			v.Set(key, value)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Auth.JWTSecret == "" {
		if secret := v.GetString("auth.jwt_secret"); secret != "" {
			cfg.Auth.JWTSecret = secret
		} else if cfg.Auth.JWTSecretEnv != "" {
			cfg.Auth.JWTSecret = os.Getenv(cfg.Auth.JWTSecretEnv)
		}
	}

	if d, err := time.ParseDuration(v.GetString("auth.token_expiration")); err == nil {
		cfg.Auth.TokenExpiration = d
	} else if cfg.Auth.TokenExpiration == 0 {
		cfg.Auth.TokenExpiration = 24 * time.Hour
	}

	return &cfg, nil
}

func (c *Config) DebugEnabled() bool {
	return strings.EqualFold(c.Logging.Level, "debug")
}