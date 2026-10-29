FROM gcr.io/distroless/static@sha256:58133991db06659feaabe0f4e97a35cebf15ef4ea08f8a4c6d2ee5f75e4aa6a0

ARG TARGETPLATFORM

WORKDIR /app

VOLUME /app/data

# workaround to prevent slowness in docker when running with a tty
ENV CI="1"

EXPOSE 3002/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/jellysweep", "healthcheck"]

ENTRYPOINT [ "/usr/local/bin/jellysweep"]
CMD [ "serve" ]

COPY $TARGETPLATFORM/jellysweep /usr/local/bin/jellysweep
