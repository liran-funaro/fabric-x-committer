# Helper tool for monitoring

This tool start a Prometheus and Graphana instance using docker.
Configuration files are located in `config/` and loaded into the docker containers on startup.

## Start monitoring

Define the machines you want to scrape from in `config/prometheus.yml`.
For example:

```yaml
    static_configs:
      - targets: [
        'tokentestbed1.sl.cloud9.ibm.com:9100',
        'tokentestbed2.sl.cloud9.ibm.com:9100',
        'tokentestbed3.sl.cloud9.ibm.com:9100',
        'tokentestbed4.sl.cloud9.ibm.com:9100',
        'tokentestbed5.sl.cloud9.ibm.com:9100',
        'tokentestbed6.sl.cloud9.ibm.com:9100',
        'tokentestbed7.sl.cloud9.ibm.com:9100',
        'tokentestbed8.sl.cloud9.ibm.com:9100'
      ]
```

Next start the monitoring containers by running:

```bash
go run cmd/main.go
```

This command will download the Prometheus and Graphana docker images and start a container.

Now you should be able to access ...
- Prometheus client running on http://localhost:9091
- Grafana running on http://localhost:3001 (user: admin, pass: admin)


## Stop monitoring

```bash
➜  monitoring git:(main) ✗ docker ps
CONTAINER ID   IMAGE                    COMMAND                  CREATED          STATUS          PORTS                    NAMES
55a3346cc9c8   grafana/grafana:latest   "/run.sh"                23 minutes ago   Up 23 minutes   0.0.0.0:3001->3000/tcp   grafana-instance
b50c0e47017a   prom/prometheus:latest   "/bin/prometheus --c…"   23 minutes ago   Up 23 minutes   0.0.0.0:9091->9090/tcp   prometheus-instance
```

```bash
docker kill 55a3346cc9c8 b50c0e47017a
docker rm 55a3346cc9c8 b50c0e47017a
```