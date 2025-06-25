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
