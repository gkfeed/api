#!/bin/sh
set -eu

CDPATH= cd "$(dirname "$0")"
exec go run github.com/swaggo/swag/cmd/swag@v1.8.12 init \
    -g cmd/api/main.go --parseInternal -o cmd/api/docs
