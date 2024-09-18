FROM golang:1.23.0-bookworm as sc_builder

ARG SERVICE_NAME

ADD . scalable-commiter
RUN cd scalable-commiter && \
  CGO_ENABLED=0 make ${SERVICE_NAME} output_dir=/root/build

FROM registry.access.redhat.com/ubi8/ubi-micro:latest

ARG SERVICE_NAME
ARG PORTS

WORKDIR /
COPY --from=sc_builder /root/build/${SERVICE_NAME} /app
EXPOSE ${PORTS}
CMD ["./app"]

