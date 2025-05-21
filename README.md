# Hetzner Restic

Schedule and monitor Restic backups and S3 snapshots with client-side encryption and WORM protection.

## Getting Started

- terraform
- config.yml
- .env
- docker-compose.yml

## Configuration

## Disaster recovery

S3 snapshots are encrypted with age using the provided passphrase. To decrypt a backup run:

```bash
age -d -o your-backup-name.tar.gz your-backup-name.tar.gz.age
# Enter your passphrase
```

## Monitoring

Prometheus metrics are exported under localhost:2112/metrics

## Grafana

A prebuilt dashboard is [here](dashboard.json)
