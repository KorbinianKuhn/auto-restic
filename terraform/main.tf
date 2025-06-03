terraform {
  required_providers {
    minio = {
      source  = "aminueza/minio"
      version = "3.3.0"
    }
    local = {
      source  = "hashicorp/local"
      version = ">= 2.0.0"
    }
  }
}

variable "endpoint" {}
variable "region" {}
variable "access_key" {}
variable "secret_key" {}
variable "validity_period" {
  default = 90 # days before an object can be deleted
}

provider "minio" {
  minio_server   = var.endpoint
  minio_user     = var.access_key
  minio_password = var.secret_key
  minio_region   = var.region
  minio_ssl      = true
}

resource "random_uuid" "id" {}

resource "minio_s3_bucket" "bucket" {
  bucket         = random_uuid.id.result
  acl            = "private"
  object_locking = true
}

resource "minio_s3_bucket_retention" "retention" {
  bucket          = minio_s3_bucket.bucket.id
  mode            = "GOVERNANCE"
  unit            = "DAYS"
  validity_period = var.validity_period
}
resource "null_resource" "lifecycle_policy" {
  depends_on = [minio_s3_bucket.bucket]

  provisioner "local-exec" {
    command = "curl -X PUT -sS --fail --user '${var.access_key}:${var.secret_key}' --aws-sigv4 'aws:amz:${var.region}:s3' -d '@lifecycle_rules.xml' https://${minio_s3_bucket.bucket.id}.${var.endpoint}/?lifecycle"
  }
}

resource "local_file" "bucket_name" {
  filename = "${path.module}/bucket_config.json"
  content = jsonencode({
    bucket   = "${minio_s3_bucket.bucket.id}",
    endpoint = "${var.endpoint}",
  })
}
