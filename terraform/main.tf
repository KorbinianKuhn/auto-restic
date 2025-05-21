terraform {
  required_providers {
    minio = {
      source = "aminueza/minio"
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

provider "minio" {
  minio_server   = "${var.endpoint}"
  minio_user     = "${var.access_key}"
  minio_password = "${var.secret_key}"
  minio_region   = "${var.region}"
  minio_ssl      = true
}

resource "random_uuid" "id" {}

resource "minio_s3_bucket" "bucket" {
  bucket         = random_uuid.id.result
  acl            = "private"
  object_locking = true
}

resource "minio_s3_bucket_retention" "retention" {
  bucket = minio_s3_bucket.bucket.id
  mode = "GOVERNANCE"
  unit = "DAYS"
  validity_period = 1 # TODO: Export to variable
}

resource "minio_s3_bucket_versioning" "versioning" {
  bucket = minio_s3_bucket.bucket.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "minio_ilm_policy" "lifecyle_policy" {
  depends_on = [minio_s3_bucket.bucket]
  bucket = minio_s3_bucket.bucket.id

  rule {
    id = "expiry"
    status = "Enabled"
    expiration = "30d"
    
    noncurrent_expiration {
        days = "60d"
    }
  }
}

resource "local_file" "bucket_name" {
  filename = "${path.module}/${minio_s3_bucket.bucket.id}.json"
  content  = jsonencode({
    bucket = "${minio_s3_bucket.bucket.id}",
    endpoint = "${var.endpoint}",
  })
}