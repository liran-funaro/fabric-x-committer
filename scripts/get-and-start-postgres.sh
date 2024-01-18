#!/bin/bash

echo "Downloading Postgres"
docker pull postgres:16.1

echo "Running Postgres container"
docker run --name sc_postgres_unit_tests \
  -e POSTGRES_PASSWORD=yugabyte \
  -e POSTGRES_USER=yugabyte \
  -p 5433:5432 \
  -d postgres:16.1
