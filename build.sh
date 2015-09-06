#!/bin/bash

go-bindata -o bindata.go -tags '!debug' templates gipam.css
go-bindata -o bindata_debug.go -tags 'debug' -debug templates gipam.css
exec go build $@ .
