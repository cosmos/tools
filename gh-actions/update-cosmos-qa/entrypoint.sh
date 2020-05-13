#!/bin/sh

cd "$GITHUB_WORKSPACE"

git clone https://git:${INPUT_GITHUB_TOKEN}@github.com/cosmos/qa.cosmos.network.git

if [ -z "$INPUT_CUSTOM_FILE_NAME" ]
then
    FILENAME=$(echo issues_${GITHUB_REPOSITORY%/*}_${GITHUB_REPOSITORY##*/}_${GITHUB_REF:11}.json);
else
    FILENAME=$INPUT_CUSTOM_FILE_NAME;
fi

golangci-lint run --out-format=json --timeout=${INPUT_GOLANGCI_TIMEOUT} --presets=${INPUT_GOLANGCI_PRESETS} > qa.cosmos.network/public/data/$FILENAME

cd qa.cosmos.network
if [ $(git status --porcelain | wc -l) -gt 0 ]
then
    git config user.name tenderbot;
    git config user.email tenderbot@tendermint.com;
    git add public/data/$FILENAME;
    git commit -m "data: add $FILENAME";
    git push;
fi
