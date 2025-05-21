package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"

	"github.com/spf13/viper"
)

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
	LocalPath  string `mapstructure:"local_path"`
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
	PreCommand  string `mapstructure:"pre_command"`
	PostCommand string `mapstructure:"post_command"`
}

type Config struct {
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
	_ = v.BindEnv("s3.local_path")
	_ = v.BindEnv("s3.access_key")
	_ = v.BindEnv("s3.secret_key")
	_ = v.BindEnv("s3.endpoint")
	_ = v.BindEnv("s3.bucket")
	_ = v.BindEnv("s3.passphrase")

	// Default values
	v.SetDefault("restic.repository", "/restic")
	v.SetDefault("restic.keep_daily", 7)
	v.SetDefault("restic.keep_weekly", 4)
	v.SetDefault("restic.keep_monthly", 3)
	v.SetDefault("cron.backup", "0 0 2 * * *")  // Every day at 2 AM
	v.SetDefault("cron.check", "0 0 2 * * 0")   // Every Sunday at 2 AM
	v.SetDefault("cron.prune", "0 0 2 * * 0")   // Every Sunday at 2 AM
	v.SetDefault("cron.s3", "0 0 2 * * 0")      // Every Sunday at 2 AM
	v.SetDefault("cron.metrics", "0 0 0 * * *") // Every day at 0 AM
	v.SetDefault("metrics_enabled", true)
	v.SetDefault("s3.local_path", "/s3")

	// Optionally load config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return config, fmt.Errorf("error reading config file: %w", err)
		}
	}

	if err := v.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("unable to decode config into struct: %w", err)
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
	for _, backup := range config.Backups {
		if backup.Path == "" {
			return config, fmt.Errorf("backup path is required")
		}
		names := make(map[string]bool)
		for _, b := range config.Backups {
			if names[b.Name] {
				return config, fmt.Errorf("duplicate backup name: %s", b.Name)
			}
			names[b.Name] = true
		}
	}

	return config, nil
}
