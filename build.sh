#!/bin/bash

if [ -z $(which godep) ]; then
    echo >&2 "install godep before building"
    echo >&2
    echo >&2 "    go get github.com/tools/godep"
    echo >&2
    exit 1
fi

godep go build -o bin/snuggied ./cmd/snuggied
godep go build -o bin/snuggier ./cmd/snuggier
