# S3 Bucket instructions

## Bucket creation

1. Copy `config.auto.tfvars.example` to `config.auto.tfvars` and adjust variables
2. `terraform init`
3. `terraform plan`
4. `terraform apply`

## Debugging

1. Install minio client
2. Optionally set variables in the terraform setup to a minimum of 1 day, to see results of object retention and lifecycle policies faster
3. Run the following steps:

```bash
# Set an alias for your s3 server (e.g. minio, hetzner)
- mc alias MY_ALIAS ENDPOINT ACCESS_KEY SECRET_KEY

# List the applied rules
- mc ilm rule ls MY_ALIAS/BUCKET_ID

# Create a test file
- echo "v1" > test.txt
- mc cp test.txt MY_ALIAS/BUCKET_ID/test.txt

# Overwrite the file
- echo "v2" > test.txt
- mc cp test.txt MY_ALIAS/BUCKET_ID/test.txt

# List files and versions of the bucket
- mc ls -- versions MY_ALIAS/BUCKET_ID

# Wait for at least one day to list files again and see the effects of lifecycle rules
```
