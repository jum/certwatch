#!/bin/bash
go run github.com/cespare/reflex -d none -s -g .env -- bash -c ". .env; go run github.com/cespare/reflex -d none -s -G .env -G \$CERTDIR -- go run . -debug -cmd='echo cert reload' -redisurl=\$REDISURL -certdir=\$CERTDIR \$CERTS"
