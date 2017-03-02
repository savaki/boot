# boot

Manage docker container secrets using S3, IAM, and KMS.

### Installation

```
go get github.com/savaki/boot
```

### Usage 

Push contents of the local config directory to the specified S3 bucket and KMS key

```
boot push --dir config --s3 s3://my-bucket/my-app --kms 12315-121251-1287982-9280912
```

### Environment Variables

All of the configuration flags can be supplied via the environment.  The following is a list of all the 
environment variables and their usage. 

Name | Description | Example | Default | Required?
:--- | :--- | :--- | :--- | :--- |
AWS_DEFAULT_REGION | AWS region containing the S3 bucket to read from | us-west-2 | us-east-1 | -
AWS_ACCESS_KEY_ID | AWS access key id; not required if using roles |  | | -
AWS_SECRET_ACCESS_KEY | AWS secret access key; not required if using roles | | | -
BOOT_ENV  | Name of environment | production, staging, etc | dev | -
BOOT_FILE | Name of file containing environment variables | | boot.env | -
BOOT_REVISION | Which version of the secret to use | 20170301.1607 | latest | -
BOOT_KMS | KMS ID to use for encryption/decryption |  | | yes
BOOT_S3 | AWS S3 bucket to read/write secrets to | s3://my-bucket/app-name | | yes
BOOT_DIR | local directory to read/write contents to | / | . | -
BOOT_VERBOSE | print additional log messages | true | false | -
BOOT_DRYRUN_VERBOSE | go through the motions, but don't upload/download anything| true | false | -

