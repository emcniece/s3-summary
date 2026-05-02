# Distroless static-debian12:nonroot is ~2MB, has /etc/passwd entries for the
# nonroot user (UID 65532), and includes ca-certificates so the AWS SDK can
# verify TLS to AWS endpoints. The binary is built by goreleaser with
# CGO_ENABLED=0 so it runs without glibc.
FROM gcr.io/distroless/static-debian12:nonroot

COPY s3-summary /usr/local/bin/s3-summary

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/s3-summary"]
