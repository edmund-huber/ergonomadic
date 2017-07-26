#!/bin/sh

set -e

if [ ! -f ircd.yaml ]; then
  cp ergonomadic.yaml ircd.yaml
fi

if [ ! -f ircd.db ]; then
  ergonomadic initdb
fi

exec ergonomadic run
