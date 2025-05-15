package config

import (
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	ResticRepository    string `default:"/restic" split_words:"true"`
	ResticPassword      string `required:"true" split_words:"true"`
	ResticBackupSources string `default:"/data" split_words:"true"`
	ResticKeepDaily     int    `default:"7" split_words:"true"`
	ResticKeepWeekly    int    `default:"4" split_words:"true"`
	ResticKeepMonthly   int    `default:"3" split_words:"true"`
	BackupCron          string `default:"0 0 2 * * *" split_words:"true"`
	CheckCron           string `default:"0 0 2 * * 0" split_words:"true"`
	PruneCron           string `default:"0 0 2 * * 0" split_words:"true"`
	S3Cron              string `default:"0 0 2 * * 0" split_words:"true"`
	MetricsEnabled      bool   `default:"true" split_words:"true"`
	S3LocalPath         string `default:"/s3" split_words:"true"`
}

func Get() (*Config, error) {
	godotenv.Load()

	var config Config

	if err := envconfig.Process("", &config); err != nil {
		return nil, err
	}

	return &config, nil
}
