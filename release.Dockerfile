FROM gcr.io/distroless/static

WORKDIR /app

VOLUME /app/data

# workaround to prevent slowness in docker when running with a tty
ENV CI="1"

EXPOSE 3002/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/jellysweep", "healthcheck"]

ENTRYPOINT [ "/usr/local/bin/jellysweep"]
CMD [ "serve" ]

COPY jellysweep /usr/local/bin/jellysweep
