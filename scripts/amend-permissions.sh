#!/bin/bash

# Workaround for issue when binding folders from host

if [ "$(uname -s)" == "Linux" ]; then
  sudo chown -R "$(id -u):$(id -g)" "$@"
fi

chmod -R +w "$@"
