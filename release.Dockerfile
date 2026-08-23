FROM gcr.io/distroless/static@sha256:f2ea2709ac8db56323cbd7d014277f32cb572d9ea124b0076f7aafe5980678fe

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
