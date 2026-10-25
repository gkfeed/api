#!/bin/sh
set -eu

# Keep CLI versions here so local commands, CI and Docker use the same tools.
swag_version=v1.8.12
swagger_version=v0.36.0
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

case "${1:-}" in
    generate)
        app_dir=$(CDPATH= cd -- "${2:-"$repo_dir/app"}" && pwd)
        output_dir=${3:-"$app_dir/cmd/api/docs"}
        cd "$app_dir"
        exec go run "github.com/swaggo/swag/cmd/swag@$swag_version" init \
            --generalInfo cmd/api/main.go --parseInternal --generatedTime=false \
            --packageName docs --outputTypes go,json,yaml --output "$output_dir"
        ;;
    validate)
        document=${2:-"$repo_dir/app/cmd/api/docs/swagger.json"}
        exec go run "github.com/go-swagger/go-swagger/cmd/swagger@$swagger_version" \
            validate "$document"
        ;;
    diff)
        if [ "$#" -ne 3 ]; then
            echo "usage: $0 diff BASE_JSON HEAD_JSON" >&2
            exit 2
        fi
        exec go run "github.com/go-swagger/go-swagger/cmd/swagger@$swagger_version" \
            diff --break "$2" "$3"
        ;;
    *)
        echo "usage: $0 generate [APP_DIR [OUTPUT_DIR]] | validate [JSON] | diff BASE_JSON HEAD_JSON" >&2
        exit 2
        ;;
esac
