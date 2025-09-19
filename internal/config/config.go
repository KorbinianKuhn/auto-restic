package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"github.com/spf13/viper"
)

type LogFormat string

const (
	LogFormatText    LogFormat = "text"
	LogFormatJSON    LogFormat = "json"
	LogFormatConsole LogFormat = "console"
)

func (f *LogFormat) UnmarshalText(text []byte) error {
	switch format := string(text); format {
	case string(LogFormatText), string(LogFormatJSON), string(LogFormatConsole):
		*f = LogFormat(format)
		return nil
	default:
		return fmt.Errorf("invalid log format: %s", format)
	}
}

type LoggingConfig struct {
	Level     string     `mapstructure:"level"`
	Format    LogFormat  `mapstructure:"format"`
	SlogLevel slog.Level `mapstructure:"-"`
}

func (c *LoggingConfig) Parse() error {
	switch strings.ToLower(c.Level) {
	case "debug":
		c.SlogLevel = slog.LevelDebug
	case "info":
		c.SlogLevel = slog.LevelInfo
	case "warn":
		c.SlogLevel = slog.LevelWarn
	case "error":
		c.SlogLevel = slog.LevelError
	default:
		return fmt.Errorf("invalid log level: %s", c.Level)
	}
	return nil
}

type ResticConfig struct {
	Password    string `mapstructure:"password"`
	Repository  string `mapstructure:"repository"`
	KeepDaily   int    `mapstructure:"keep_daily"`
	KeepWeekly  int    `mapstructure:"keep_weekly"`
	KeepMonthly int    `mapstructure:"keep_monthly"`
}

type CronConfig struct {
	Backup  string `mapstructure:"backup"`
	Check   string `mapstructure:"check"`
	Prune   string `mapstructure:"prune"`
	S3      string `mapstructure:"s3"`
	Metrics string `mapstructure:"metrics"`
}

type S3Config struct {
	AccessKey  string `mapstructure:"access_key"`
	SecretKey  string `mapstructure:"secret_key"`
	Endpoint   string `mapstructure:"endpoint"`
	Bucket     string `mapstructure:"bucket"`
	Passphrase string `mapstructure:"passphrase"`
}

type BackupConfig struct {
	Name        string `mapstructure:"name"`
	Path        string `mapstructure:"path"`
	Exclude     string `mapstructure:"exclude"`
	ExcludeFile string `mapstructure:"exclude_file"`
	PreCommand  string `mapstructure:"pre_command"`
	PostCommand string `mapstructure:"post_command"`
}

type Config struct {
	Logging        LoggingConfig  `mapstructure:"logging"`
	Restic         ResticConfig   `mapstructure:"restic"`
	Cron           CronConfig     `mapstructure:"cron"`
	S3             S3Config       `mapstructure:"s3"`
	MetricsEnabled bool           `mapstructure:"metrics_enabled"`
	Backups        []BackupConfig `mapstructure:"backups"`
}

func Get() (Config, error) {
	var config Config

	godotenv.Load()

	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind environment variables
	_ = v.BindEnv("logging.level")
	_ = v.BindEnv("logging.format")
	_ = v.BindEnv("restic.password")
	_ = v.BindEnv("restic.repository")
	_ = v.BindEnv("restic.keep_daily")
	_ = v.BindEnv("restic.keep_weekly")
	_ = v.BindEnv("restic.keep_monthly")
	_ = v.BindEnv("cron.backup")
	_ = v.BindEnv("cron.check")
	_ = v.BindEnv("cron.prune")
	_ = v.BindEnv("cron.s3")
	_ = v.BindEnv("cron.metrics")
	_ = v.BindEnv("metrics_enabled")
	_ = v.BindEnv("s3.access_key")
	_ = v.BindEnv("s3.secret_key")
	_ = v.BindEnv("s3.endpoint")
	_ = v.BindEnv("s3.bucket")
	_ = v.BindEnv("s3.passphrase")

	// Default values
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
	v.SetDefault("restic.repository", "/repository")
	v.SetDefault("restic.keep_daily", 7)
	v.SetDefault("restic.keep_weekly", 4)
	v.SetDefault("restic.keep_monthly", 3)
	v.SetDefault("cron.metrics", "0 0 0 * * *") // Every day at 00:00
	v.SetDefault("cron.backup", "0 0 2 * * *")  // Every day at 02:00
	v.SetDefault("cron.s3", "0 1 2 * * 0")      // Every Sunday 02:01
	v.SetDefault("cron.check", "0 2 2 * * 0")   // Every Sunday 02:02
	v.SetDefault("cron.prune", "0 3 2 * * 0")   // Every Sunday 02:03
	v.SetDefault("metrics_enabled", true)

	// Optionally load config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return config, fmt.Errorf("error reading config file: %w", err)
		}
	}

	if err := v.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("unable to decode config into struct: %w", err)
	}

	if err := config.Logging.Parse(); err != nil {
		return config, fmt.Errorf("invalid logging configuration: %w", err)
	}

	// Set logger
	switch config.Logging.Format {
	case LogFormatText:
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: config.Logging.SlogLevel,
		})))
	case LogFormatJSON:
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: config.Logging.SlogLevel,
		})))
	default:
		slog.SetLogLoggerLevel(config.Logging.SlogLevel)
	}

	if config.Restic.Password == "" {
		return config, fmt.Errorf("RESTIC_PASSWORD is required")
	}

	if config.S3.AccessKey == "" {
		return config, fmt.Errorf("S3_ACCESS_KEY is required")
	}

	if config.S3.SecretKey == "" {
		return config, fmt.Errorf("S3_SECRET_KEY is required")
	}

	if config.S3.Endpoint == "" {
		return config, fmt.Errorf("S3_ENDPOINT is required")
	}

	if config.S3.Bucket == "" {
		return config, fmt.Errorf("S3_BUCKET is required")
	}

	if config.S3.Passphrase == "" {
		return config, fmt.Errorf("S3_PASSPHRASE is required")
	}

	// Validate backup configurations
	names := make(map[string]bool)
	for _, backup := range config.Backups {
		if backup.Path == "" {
			return config, fmt.Errorf("backup path is required")
		}

		if names[backup.Name] {
			return config, fmt.Errorf("duplicate backup name: %s", backup.Name)
		}
		names[backup.Name] = true

		if backup.ExcludeFile != "" {
			_, err := os.Stat(backup.ExcludeFile)
			if os.IsNotExist(err) {
				return config, fmt.Errorf("exclude file does not exist: %s", backup.ExcludeFile)
			}
		}

		_, err := os.Stat(backup.Path)
		if os.IsNotExist(err) {
			slog.Warn("backup path does not exist yet", "path", backup.Path)
		}
	}

	return config, nil
}
