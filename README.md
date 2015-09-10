![Unsupported](https://img.shields.io/badge/development_status-unsupported-red.svg) ![License BSDv2](https://img.shields.io/badge/license-BSDv2-brightgreen.svg)

Roadcrew is a simple Go command to continusouly run the *amazing* Sysdig, uploading the results to S3.

## Usage:
```
roadcrew [options] -i <interval> -b <bucket_name>
roadcrew -h --help
roadcrew --version
```

## Options:
```
-K <aws_key_id>       AWS Key ID.
-S <aws_secret_key>   AWS Secret Key.
-t <tmp_dir>          Specify tmp directory.
-h, --help            Show this screen.
--version             Show version.
```

## Environment variables:
Either set these environment variables or use the -S and -K flags.
* export AWS_ACCESS_KEY_ID="AAAAAAAAAAAAAAAAAAAAA"
* export AWS_SECRET_ACCESS_KEY="BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
