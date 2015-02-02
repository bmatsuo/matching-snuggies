#!/bin/bash

GO="go"
if which godep > /dev/null; then
    echo >&2 "building with godep"
    GO="godep go"
else
    echo >&2 "you should install godep"
    echo >&2
    echo >&2 "    go get github.com/tools/godep"
    echo >&2
    echo >&2 "attempting to build anyway..."
fi

$GO build -o bin/snuggied ./cmd/snuggied
$GO build -o bin/snuggier ./cmd/snuggier
