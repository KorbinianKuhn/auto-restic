terraform {
  required_providers {
    minio = {
      source  = "aminueza/minio"
      version = "3.45.0"
    }
    local = {
      source  = "hashicorp/local"
      version = ">= 2.0.0"
    }
    random = {
      source = "hashicorp/random"
    }
    null = {
      source = "hashicorp/null"
    }
  }
}

variable "endpoint" {}
variable "region" {}
variable "access_key" {
  sensitive = true
}
variable "secret_key" {
  sensitive = true
}
variable "object_retention_days" {
  type        = number
  description = "Number of days to retain objects under governance mode (S3 object lock)."
}

variable "lifecycle_expiration_days" {
  type        = number
  description = "Number of days after which current object versions are expired via lifecycle."
}

variable "noncurrent_version_expiration_days" {
  type        = number
  description = "Number of days after which noncurrent (previous) versions are deleted via lifecycle."
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
  validity_period = var.object_retention_days
}

resource "local_file" "lifecycle_rules" {
  filename = "${path.module}/lifecycle_rules.xml"
  content = templatefile("${path.module}/lifecycle_rules.xml.tftpl", {
    lifecycle_expiration_days          = var.lifecycle_expiration_days
    noncurrent_version_expiration_days = var.noncurrent_version_expiration_days
  })
}

resource "null_resource" "lifecycle_policy" {
  depends_on = [minio_s3_bucket.bucket]

  triggers = {
    template = base64sha256(local_file.lifecycle_rules.content)
  }

  # Credentials are passed via environment, so they don't show up in the
  # logged command
  provisioner "local-exec" {
    command = "curl -X PUT -sS --fail --user \"$S3_ACCESS_KEY:$S3_SECRET_KEY\" --aws-sigv4 'aws:amz:${var.region}:s3' -d '@${abspath(local_file.lifecycle_rules.filename)}' https://${minio_s3_bucket.bucket.id}.${var.endpoint}/?lifecycle"
    environment = {
      S3_ACCESS_KEY = var.access_key
      S3_SECRET_KEY = var.secret_key
    }
  }
}

resource "local_file" "bucket_name" {
  filename = "${path.module}/bucket_config.json"
  content = jsonencode({
    bucket   = "${minio_s3_bucket.bucket.id}",
    endpoint = "${var.endpoint}",
  })
}
