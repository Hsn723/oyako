FROM scratch
LABEL org.opencontainers.image.authors="Hsn723" \
      org.opencontainers.image.title="oyako" \
      org.opencontainers.image.source="https://github.com/hsn723/oyako"
WORKDIR /
COPY oyako /
COPY LICENSE /LICENSE
USER 65532:65532

ENTRYPOINT ["/oyako"]
