#!/bin/bash
go run github.com/cespare/reflex -d none -s -g .env -- bash -c ". .env; go run github.com/cespare/reflex -d none -s -G .env -G \$CERTDIR -- go run . -debug -redisurl=\$REDISURL -certdir=\$CERTDIR \$CERTS"
