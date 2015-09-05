#!/bin/bash

exec go-compile-daemon -directory=. -exclude 'bindata*.go' -build="./build.sh -tags=debug" -command="./gipam -debug" -color
