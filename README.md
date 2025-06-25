# Hetzner Restic

Schedule and monitor Restic backups and S3 snapshots with client-side encryption and WORM (Write Once Read Many) protection.

## Getting Started

1. Create an s3 bucket with retention mode (governance) and lifecycle policies: [see terraform module](terraform)
2. Create a `config.yml`, `.env` and `docker-compose.yml`: Examples below
3. Configure your backup directories and pre/post commands in your `config.yml`
4. Run `docker compose up`

## Limitations

- ⚠️ Docker Desktop is not supported (restic will complain about file reads despite correct mounts)

## Configuration

Configuration can be passed through environment variables and a config.yml. It's recommended to use `.env` for sensitive values (e.g. credentials) and `config.yml` for all other values.

### Example

.env (all variables are required)

```env
RESTIC_PASSWORD=
S3_ACCESS_KEY=
S3_SECRET_KEY=
S3_ENDPOINT=
S3_BUCKET=
S3_PASSPHRASE=
```

config.yml (all variables are shown with defaults and are optional)

```yaml
restic:
  repository: /repository
  keep_daily: 7
  keep_weekly: 4
  keep_monthly: 3

cron:
  metrics: "0 0 0 * * *" # Every day at 00:00
  backup: "0 0 2 * * *" # Every day at 02:00
  s3: "0 1 2 * * 0" # Every Sunday 02:01
  check: "0 2 2 * * 0" # Every Sunday 02:01
  prune: "0 3 2 * * 0" # Every Sunday 02:03

backups:
  - path: /data/mongodb-dump
    name: mongodb-dump
    pre_command: "docker exec -i mongodb mongodump --archive=/mongodb-dump/mongodb-dump.archive"
    post_command: "docker exec -i mongodb rm /mongodb-dump/mongodb-dump.archive"
```

docker-compose.yml

```yaml
services:
  hetzner-restic:
    image: ghcr.io/korbiniankuhn/hetzner-restic:1.0.0
    container_name: hetzner-restic
    ports:
      - 127.0.0.1:2112:2112
    volumes:
      - ./.env:/hetzner-restic/.env:ro
      - ./config.yml:/hetzner-restic/config.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - ./restic:/repository
```

## Restore

### Restic

TODO

### S3 (Disaster Recovery)

S3 snapshots are encrypted with age using the provided passphrase. To decrypt a backup run:

```bash
age -d -o your-backup-name.tar.gz your-backup-name.tar.gz.age
# Enter your passphrase
```

## Monitoring

Prometheus metrics are exported under localhost:2112/metrics

TODO: Example

## Grafana

TODO: Image and dashboard.json

A prebuilt dashboard is [here](dashboard.json)

## Credits

- [https://github.com/go-co-op/gocron](https://github.com/go-co-op/gocron)
- [https://github.com/spf13/viper](https://github.com/spf13/viper)
- [https://github.com/FiloSottile/age](https://github.com/FiloSottile/age)
