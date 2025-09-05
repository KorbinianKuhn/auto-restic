# AutoRestic

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
logging:
  level: info # one of (debug, info, warn, error)
  format: text # one of (text, json, console)

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
  auto-restic:
    image: ghcr.io/korbiniankuhn/auto-restic:1.0.0
    container_name: auto-restic
    ports:
      - 127.0.0.1:2112:2112
    volumes:
      - ./.env:/auto-restic/.env:ro
      - ./config.yml:/auto-restic/config.yml:ro
      - ./data:/data
      - ./restic:/repository
      - /var/run/docker.sock:/var/run/docker.sock # only required if you run docker commands for pre-post backup scripts
```

## CLI

A CLI is provided to list, remove, and restore backups (restic or S3). The CLI uses the same config as the server (e.g. for access keys, secrets).

Connect to a running container

```bash
docker exec -it auto-restic sh
```

or start a temporary one:

```bash
docker compose run --entrypoint "/bin/sh" auto-restic
```

| Command                                                          | Description                                 |
| ---------------------------------------------------------------- | ------------------------------------------- |
| ./cli restic ls                                                  | List all local backups and snapshots        |
| ./cli restic rm --name ""                                        | Remove all snapshots of a backup            |
| ./cli restic restore --snapshot-id "" --mount-path ""            | Restore snapshot to a local directory       |
| ./cli s3 ls                                                      | List all S3 backups and versions            |
| ./cli s3 rm --object-key "" --version-id ""                      | Remove S3 object with specific version      |
| ./cli s3 restore --object-key "" --version-id "" --mount-path "" | Restore object version to a local directory |

### S3 (Disaster Recovery)

S3 snapshots are encrypted with age using the provided passphrase. To decrypt a backup without using the CLI run:

```bash
age -d -o your-backup-name.tar.gz your-backup-name.tar.gz.age
# Enter your passphrase
```

## Monitoring

Prometheus metrics are exported under [localhost:2112/metrics](localhost:2112/metrics)

```yaml
# HELP restic_repository_snapshot_latest_size_bytes Size of the latest snapshot per backup name
# TYPE restic_repository_snapshot_latest_size_bytes gauge
restic_repository_snapshot_latest_size_bytes{name="mongodb-dump"} 1240
restic_repository_snapshot_latest_size_bytes{name="production"} 0
restic_repository_snapshot_latest_size_bytes{name="staging"} 0
# HELP restic_repository_snapshot_latest_time Timestamp of the latest snapshot per backup name
# TYPE restic_repository_snapshot_latest_time gauge
restic_repository_snapshot_latest_time{name="mongodb-dump"} 1.749808336e+09
restic_repository_snapshot_latest_time{name="production"} 1.7568957e+09
restic_repository_snapshot_latest_time{name="staging"} 1.7568957e+09
# HELP restic_repository_snapshot_total Number of snapshots per backup name
# TYPE restic_repository_snapshot_total gauge
restic_repository_snapshot_total{name="mongodb-dump"} 2
restic_repository_snapshot_total{name="production"} 4
restic_repository_snapshot_total{name="staging"} 2
# HELP restic_repository_snapshot_total_size_bytes Total size of all snapshots per backup name
# TYPE restic_repository_snapshot_total_size_bytes gauge
restic_repository_snapshot_total_size_bytes{name="mongodb-dump"} 2527
restic_repository_snapshot_total_size_bytes{name="production"} 2180
restic_repository_snapshot_total_size_bytes{name="staging"} 1111
# HELP restic_s3_latest_size_bytes Size of the latest snapshot dump in s3 per backup name
# TYPE restic_s3_latest_size_bytes gauge
restic_s3_latest_size_bytes{name="mongodb-dump"} 711
restic_s3_latest_size_bytes{name="production"} 395
restic_s3_latest_size_bytes{name="staging"} 389
# HELP restic_s3_latest_time Timestamp of the latest snapshot dump in s3 per backup name
# TYPE restic_s3_latest_time gauge
restic_s3_latest_time{name="mongodb-dump"} 1.756895707e+09
restic_s3_latest_time{name="production"} 1.756895705e+09
restic_s3_latest_time{name="staging"} 1.756895706e+09
# HELP restic_s3_total Number of snapshot dumps in s3 per backup name
# TYPE restic_s3_total gauge
restic_s3_total{name="mongodb-dump"} 3
restic_s3_total{name="production"} 3
restic_s3_total{name="staging"} 3
# HELP restic_s3_total_size_bytes Total size of all snapshot dumps in s3 per backup name
# TYPE restic_s3_total_size_bytes gauge
restic_s3_total_size_bytes{name="mongodb-dump"} 2133
restic_s3_total_size_bytes{name="production"} 1185
restic_s3_total_size_bytes{name="staging"} 1167
```

## Grafana

A prebuilt dashboard is [here](dashboard.json)

![Screenshot of the Grafana dashboard for AutoRestic](dashboard.png)

## Credits

- [https://github.com/go-co-op/gocron](https://github.com/go-co-op/gocron)
- [https://github.com/spf13/viper](https://github.com/spf13/viper)
- [https://github.com/spf13/cobra](https://github.com/spf13/cobra)
- [https://github.com/FiloSottile/age](https://github.com/FiloSottile/age)
